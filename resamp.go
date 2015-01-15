package ffau

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"unsafe"
)

/*
#include <libavformat/avformat.h>
#include <libswresample/swresample.h>
*/
import "C"

type AudioFormat struct {
	Rate int
	Storage SampleFmt
	Layout ChannelLayout
}

type ChannelLayout C.int64_t // TODO proper enum?

type Resampler struct {
	fmt AudioFormat
	ctx *C.SwrContext
	source SampleStream
	sourceEOF bool
	sratio float64
	data []*C.uint8_t
	nf C.int // samples allocated (per plane)
}

func DefaultLayout(nChannels int) ChannelLayout {
	return ChannelLayout(C.av_get_default_channel_layout(C.int(nChannels)))
}

func (this AudioFormat) Equal(that AudioFormat) bool {
	return this.Rate == that.Rate && this.Storage == that.Storage && this.Layout == that.Layout
}

func (format AudioFormat) NumChannels() int {
	return int(C.av_get_channel_layout_nb_channels(C.uint64_t(format.Layout)))
}

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
		return nil, avError(r)
	}
	return &resamp, nil
}

func (resamp *Resampler) Close() {
	if resamp.ctx != nil {
		C.swr_free(&resamp.ctx) /* sets ctx to nil */
	}
	for i, ptr := range resamp.data {
		if ptr != nil {
			C.free(unsafe.Pointer(ptr))
			resamp.data[i] = nil
		}
	}
	resamp.source.Close()
}

func (resamp *Resampler) Format() AudioFormat {
	return resamp.fmt
}

func (resamp *Resampler) checkBuf(nf C.int) error {
	if resamp.nf < nf {
		nbytes := nf * C.av_get_bytes_per_sample(int32(resamp.fmt.Storage)) * C.int(resamp.source.Format().NumPlanes())
		if resamp.nf == 0 {
			resamp.data = make([]*C.uint8_t, resamp.fmt.NumPlanes())
		} else {
			for i, ptr := range resamp.data {
				C.free(unsafe.Pointer(ptr))
				resamp.data[i] = nil
			}
		}
		for i, _ := range resamp.data {
			resamp.data[i] = (*C.uint8_t)(unsafe.Pointer((C.malloc(C.size_t(nbytes)))))
			if resamp.data[i] == nil {
				return errors.New("couldn't allocate resample output buffer")
			}
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
		}
		err = resamp.checkBuf(C.int(math.Ceil(float64(nf) * resamp.sratio)))
		if err != nil {
			return nil, 0, err
		}
	}
	h := (*reflect.SliceHeader)(unsafe.Pointer(&resamp.data))
	out := (**C.uint8_t)(unsafe.Pointer(h.Data))
	nfout := C.swr_convert(resamp.ctx, out, resamp.nf, in, nf)
	if nfout == 0 {
		return nil, 0, io.EOF
	}
	return out, nfout, nil
}

func dumpPlanarStereo(data **C.uint8_t, nf C.int) {
	ext := (*[8]*C.uint8_t)(unsafe.Pointer(data))
	l := C.GoBytes(unsafe.Pointer(ext[0]), nf * 2)
	r := C.GoBytes(unsafe.Pointer(ext[1]), nf * 2)
	for i := 0; i < int(nf); i++ {
		sl := uint16(l[i * 2]) + (uint16(l[i * 2 + 1]) << 8)
		sr := uint16(r[i * 2]) + (uint16(r[i * 2 + 1]) << 8)
		fmt.Printf("%4d %+05x %+05x\n", i, sl, sr)
	}
}

func dumpPackedStereo(data **C.uint8_t, nf C.int) {
	ext := (*[8]*C.uint8_t)(unsafe.Pointer(data))
	s := C.GoBytes(unsafe.Pointer(ext[0]), nf * 2 * 2)
	for i := 0; i < int(nf); i++ {
		sl := uint16(s[i * 4]) + (uint16(s[i*4 + 1]) << 8)
		sr := uint16(s[i * 4 + 2]) + (uint16(s[i*4 + 3]) << 8)
		fmt.Printf("%4d %+05x %+05x\n", i, sl, sr)
	}
}

