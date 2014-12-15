package ffau

import (
	"errors"
	"reflect"
	"unsafe"
)

type PackedS16Stream struct {
	source SampleStream
}

func NewPackedS16Stream(source SampleStream) (*PackedS16Stream, error) {
	if source.Format().Storage != PackedS16s {
		return nil, errors.New("sample format mismatch")
	}
	return &PackedS16Stream{source}, nil
}

func (stream PackedS16Stream) Read() ([]int16, error) {
	data, nf, err := stream.source.read_raw()
	ns := int(stream.source.Format().NumChannels()) * int(nf)
	if err != nil {
		return []int16{}, err
	}
	if data == nil {
		return []int16{}, nil
	}
	s := reflect.SliceHeader{Data: uintptr(unsafe.Pointer(*data)), Len: ns, Cap: ns}
	return *(*[]int16)(unsafe.Pointer(&s)), nil
}


/* potential target API:

AsPackedS16s(filename string, desiredSampleRate int, desiredLayout ChannelLayout) PackedS16Stream

(allows use of request_channel_layout/request_sample_fmt fields on CodecContext)

and then:

PackedS16Stream.Close() to cleanup all memory.
*/