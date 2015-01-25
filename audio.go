package ffau

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

/*
#cgo pkg-config: libavformat libavcodec libavutil libswresample
#include <libavutil/samplefmt.h>
#include <libavformat/avformat.h>
*/
import "C"

func init() {
	C.av_register_all()
}

/* FormatContext represents a decoding context of eg. an open file. */
type FormatContext struct {
	ctx *C.AVFormatContext
}

type SampleStream interface {
	/* The format in which this stream's samples are stored. */
	Format() AudioFormat

	/* Releases any resources associated with the stream. */
	Close()

	/* returns raw audio data (1 pointer per plane) and number of samples (per plane), or an error */
	read_raw() (**C.uint8_t, C.int, error)
}

type AudioStream struct {
	ctx    *FormatContext
	idx    C.int
	stream *C.AVStream
	fmt    AudioFormat

	// the original data/size when a packet is first read
	orig struct {
		data *C.uint8_t
		size C.int
	}
	pkt       C.AVPacket
	frame     *C.AVFrame
	framesEOF bool
}

func avError(errnum C.int) error {
	switch errnum {
	case C.AVERROR_EOF:
		return io.EOF
	}
	var buf [256]C.char
	cp := (*C.char)(unsafe.Pointer(&buf[0]))
	C.av_strerror(errnum, cp, C.size_t(len(buf)))
	return errors.New(C.GoString(cp))
}

/* Opens an audio file and returns a FormatContext which can be used to decode
the file. */
func OpenFile(filename string) (*FormatContext, error) {
	var ctx FormatContext
	cfile := C.CString(filename)
	defer C.free(unsafe.Pointer(cfile))
	r := C.avformat_open_input(&ctx.ctx, cfile, nil, nil)
	if r < 0 {
		return nil, avError(r)
	}
	return &ctx, nil
}

/* Closes a FormatContext, releasing associated resources. */
func (format *FormatContext) Close() {
	C.avformat_close_input(&format.ctx)
}

func (format *FormatContext) stream(index int) *C.AVStream {
	if index < 0 || C.uint(index) >= format.ctx.nb_streams {
		panic(fmt.Sprintf("stream index %d outside range [0, %d)", index, int(format.ctx.nb_streams)))
	}
	stream := ((*[1 << 10]*C.AVStream)(unsafe.Pointer(format.ctx.streams)))[index]
	return stream
}

func (format *FormatContext) findStreamInfo() error {
	r := C.avformat_find_stream_info(format.ctx, nil)
	if r < 0 {
		return avError(r)
	}
	return nil
}

/* Returns the "best" AudioStream found in the file. */
func (format *FormatContext) OpenAudioStream() (*AudioStream, error) {
	err := format.findStreamInfo()
	if err != nil {
		return nil, err
	}
	idx := C.av_find_best_stream(format.ctx, C.AVMEDIA_TYPE_AUDIO, -1, -1, nil, 0)
	if idx < 0 {
		return nil, avError(idx)
	}
	stream := format.stream(int(idx))
	dec_ctx := stream.codec
	decoder := C.avcodec_find_decoder(dec_ctx.codec_id)
	if decoder == nil {
		return nil, errors.New("No decoder available")
	}
	dict := (*C.AVDictionary)(nil)
	r := C.avcodec_open2(dec_ctx, decoder, &dict)
	if r < 0 {
		return nil, avError(r)
	}
	audio := &AudioStream{ctx: format, idx: idx, stream: stream}
	audio.frame = C.av_frame_alloc()
	if audio.frame == nil {
		return nil, errors.New("Couldn't allocate frame")
	}
	audio.fmt = AudioFormat{int(dec_ctx.sample_rate), SampleFmt(dec_ctx.sample_fmt), ChannelLayout(dec_ctx.channel_layout)}
	C.av_init_packet(&audio.pkt)
	audio.pkt.data = nil
	audio.pkt.size = 0
	return audio, nil
}

func (audio *AudioStream) Close() {
	C.avcodec_close(audio.stream.codec)
	C.av_frame_free(&audio.frame)
}

func (audio AudioStream) Format() AudioFormat {
	return audio.fmt
}

func (audio *AudioStream) read_frame() error {
	if audio.orig.data != nil {
		/* transliterated from examples/demuxing_decoding.c
		** presumably the data pointer needs to be reset before the packet is freed? */
		audio.pkt.data = audio.orig.data
		audio.pkt.size = audio.orig.size
		C.av_free_packet(&audio.pkt)
	}
	r := C.av_read_frame(audio.ctx.ctx, &audio.pkt)
	if r == 0 {
		return nil
	}
	return avError(r)
}

func (audio *AudioStream) decode() (bool, error) {
	gotFrame := C.int(0)
	n := C.avcodec_decode_audio4(audio.stream.codec, audio.frame, &gotFrame, &audio.pkt)
	if n < 0 {
		return false, avError(n)
	}
	if n > audio.pkt.size {
		n = audio.pkt.size
	}
	audio.pkt.size -= n
	audio.pkt.data = (*C.uint8_t)(unsafe.Pointer(uintptr(unsafe.Pointer(audio.pkt.data)) + uintptr(n)))
	return gotFrame != 0, nil
}

/* note: can return data chunks of length zero. error will be io.EOF at end of stream */
func (audio *AudioStream) read_raw() (**C.uint8_t, C.int, error) {
	if audio.pkt.size == 0 && !audio.framesEOF {
		err := audio.read_frame()
		if err == io.EOF {
			audio.framesEOF = true
		} else if err != nil {
			return nil, 0, err
		}
	}
	gotFrame, err := audio.decode()
	if err != nil {
		return nil, 0, err
	}
	if !gotFrame {
		if audio.framesEOF {
			return nil, 0, io.EOF
		}
		return nil, 0, nil
	}
	return audio.frame.extended_data, audio.frame.nb_samples, nil
}
