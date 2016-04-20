package ffau

import (
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"
)

/*
#include <libavformat/avformat.h>
#include <libswresample/swresample.h>
*/
import "C"

type AudioFormat struct {
	Rate    int           // Number of frames per second
	Storage SampleFmt     // Sample encoding / memory layout
	Layout  ChannelLayout // Channel configuration
}

type ChannelLayout C.int64_t // TODO proper enum?

/* Converts a SampleStream to a target AudioFormat. Resampler
itself implements the SampleStream interface. */
type Resampler struct {
	fmt       AudioFormat
	ctx       *C.SwrContext
	source    SampleStream
	sourceEOF bool
	sratio    float64
	data      **C.uint8_t
	nplanes   int
	nf        C.int // samples allocated (per plane)
	buf       *C.uint8_t
}

var sizeOfPtr int

func init() {
	e := C.uint8_t(0)
	sizeOfPtr = int(unsafe.Sizeof(&e))
}

/* Returns the "default" channel layout for the given number of channels. */
func DefaultLayout(nChannels int) ChannelLayout {
	return ChannelLayout(C.av_get_default_channel_layout(C.int(nChannels)))
}

/* Compares an AudioFormat to another. */
func (this AudioFormat) Equal(that AudioFormat) bool {
	return this.Rate == that.Rate && this.Storage == that.Storage && this.Layout == that.Layout
}

/* The number of channels the AudioFormat describes. */
func (format AudioFormat) NumChannels() int {
	return int(C.av_get_channel_layout_nb_channels(C.uint64_t(format.Layout)))
}

/* The number of storage planes the AudioFormat suggests. For packed audio
this is always 1, and for planar audio equivalent to NumChannels. */
func (format AudioFormat) NumPlanes() int {
	switch format.Storage {
	case PackedU8s, PackedS16s, PackedS32s, PackedFloats, PackedDoubles:
		return 1
	case PlanarU8s, PlanarS16s, PlanarS32s, PlanarFloats, PlanarDoubles:
		return format.NumChannels()
	default:
		return 0
	}
}

/* Creates a Resampler that converts the source SampleStream to the requested
AudioFormat. If the source is already in the requested format, then it is
returned as is (ie. no Resampler is allocated). */
func Resample(source SampleStream, to AudioFormat) (SampleStream, error) {
	resamp := Resampler{fmt: to, source: source}
	from := source.Format()
	if from.Equal(to) {
		return source, nil /* no-op */
	}
	resamp.ctx = C.swr_alloc_set_opts(nil,
		C.int64_t(to.Layout), int32(to.Storage), C.int(to.Rate),
		C.int64_t(from.Layout), int32(from.Storage), C.int(from.Rate),
		0, nil)
	if resamp.ctx == nil {
		return nil, errors.New("couldn't allocate resampling context")
	}
	resamp.sratio = float64(to.Rate) / float64(from.Rate)
	r := C.swr_init(resamp.ctx)
	if r < 0 {
		return nil, avError(r, "swr_init")
	}
	resamp.nplanes = from.NumPlanes()
	resamp.data = (**C.uint8_t)(C.malloc(C.size_t(uintptr(from.NumPlanes() * sizeOfPtr))))
	if resamp.data == nil {
		return nil, errors.New("couldn't allocate resampler channel pointers")
	}
	return &resamp, nil
}

/* Frees memory associated with a Resampler, and closes the source stream. */
func (resamp *Resampler) Close() {
	if resamp.ctx != nil {
		C.swr_free(&resamp.ctx) /* sets ctx to nil */
		C.free(unsafe.Pointer(resamp.data))
		if resamp.buf != nil {
			C.free(unsafe.Pointer(resamp.buf))
		}
	}
	resamp.source.Close()
}

func (resamp *Resampler) Format() AudioFormat {
	return resamp.fmt
}

func (resamp *Resampler) checkBuf(nf C.int) error {
	if resamp.nf < nf {
		// bpc = bytes per channel
		bpc := int(nf * C.av_get_bytes_per_sample(int32(resamp.fmt.Storage)))
		nbytes := bpc * int(resamp.fmt.NumChannels())
		if resamp.buf != nil {
			C.free(unsafe.Pointer(resamp.buf))
			resamp.buf = nil
		}
		resamp.buf = (*C.uint8_t)(C.malloc(C.size_t(nbytes)))
		if resamp.buf == nil {
			return errors.New("couldn't allocate resampler data block")
		}

		/* In the planar case, this loop sets up pointers for each plane in contiguous
		** fashion within the allocated block. In the packed case, it sets up a single
		** pointer to the start of the block. */
		data_0 := uintptr(unsafe.Pointer(resamp.data))
		buf_0 := uintptr(unsafe.Pointer(resamp.buf))
		for i := 0; i < resamp.nplanes; i++ {
			data_i := (**C.uint8_t)(unsafe.Pointer(data_0 + uintptr(i * sizeOfPtr)))
			*data_i = (*C.uint8_t)(unsafe.Pointer(buf_0 + uintptr(i * bpc)))
		}
		resamp.nf = nf
	}
	return nil
}

func (resamp *Resampler) read_raw() (**C.uint8_t, C.int, error) {
	in := (**C.uint8_t)(nil)
	nf := C.int(0)
	if !resamp.sourceEOF {
		var err error
		in, nf, err = resamp.source.read_raw()
		if err == io.EOF {
			resamp.sourceEOF = true
		} else if err != nil {
			return nil, 0, err
		} else if nf == 0 {
			return nil, 0, nil
		}
		err = resamp.checkBuf(C.int(math.Ceil(float64(nf) * resamp.sratio)))
		if err != nil {
			return nil, 0, err
		}
	}
	nfout := C.swr_convert(resamp.ctx, resamp.data, resamp.nf, in, nf)
	if nfout == 0 {
		return nil, 0, io.EOF
	}
	return resamp.data, nfout, nil
}

func dumpPlanarStereo(data **C.uint8_t, nf C.int) {
	ext := (*[8]*C.uint8_t)(unsafe.Pointer(data))
	l := C.GoBytes(unsafe.Pointer(ext[0]), nf*2)
	r := C.GoBytes(unsafe.Pointer(ext[1]), nf*2)
	for i := 0; i < int(nf); i++ {
		sl := uint16(l[i*2]) + (uint16(l[i*2+1]) << 8)
		sr := uint16(r[i*2]) + (uint16(r[i*2+1]) << 8)
		fmt.Printf("%4d %+05x %+05x\n", i, sl, sr)
	}
}

func dumpPackedStereo(data **C.uint8_t, nf C.int) {
	ext := (*[8]*C.uint8_t)(unsafe.Pointer(data))
	s := C.GoBytes(unsafe.Pointer(ext[0]), nf*2*2)
	for i := 0; i < int(nf); i++ {
		sl := uint16(s[i*4]) + (uint16(s[i*4+1]) << 8)
		sr := uint16(s[i*4+2]) + (uint16(s[i*4+3]) << 8)
		fmt.Printf("%4d %+05x %+05x\n", i, sl, sr)
	}
}
