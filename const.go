package ffau

// #include <libavutil/samplefmt.h>
import "C"

type SampleFmt int8

const (
	NoSamples     SampleFmt = C.AV_SAMPLE_FMT_NONE
	PackedU8s     SampleFmt = C.AV_SAMPLE_FMT_U8  ///< unsigned 8 bits
	PackedS16s    SampleFmt = C.AV_SAMPLE_FMT_S16 ///< signed 16 bits
	PackedS32s    SampleFmt = C.AV_SAMPLE_FMT_S32 ///< signed 32 bits
	PackedFloats  SampleFmt = C.AV_SAMPLE_FMT_FLT ///< float
	PackedDoubles SampleFmt = C.AV_SAMPLE_FMT_DBL ///< double

	PlanarU8s     SampleFmt = C.AV_SAMPLE_FMT_U8P  ///< unsigned 8 bits, planar
	PlanarS16s    SampleFmt = C.AV_SAMPLE_FMT_S16P ///< signed 16 bits, planar
	PlanarS32s    SampleFmt = C.AV_SAMPLE_FMT_S32P ///< signed 32 bits, planar
	PlanarFloats  SampleFmt = C.AV_SAMPLE_FMT_FLTP ///< float, planar
	PlanarDoubles SampleFmt = C.AV_SAMPLE_FMT_DBLP ///< double, planar
)
