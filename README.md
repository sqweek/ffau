Provides a straightforward API to open an arbitrary file, decode & convert the audio stream contained within.

Quick example (error handling elided):

```
fmtCtx, err := ffau.OpenFile(filename)
stream, err := fmtCtx.OpenAudioStream()
```

At this point `stream` is in whatever format the audio decoder chose. To convert to the desired format use the `Resample` function. Eg. to convert to stereo signed 16-bit samples @ 44100Hz:

```
stereo := ffau.AudioFormat{44100, ffau.PackedS16s, ffau.DefaultLayout(2))}
resampled, err := ffau.Resample(stream, stereo)
```

`Resampled` and `stream` are both of type SampleStream, which provides no direct way to get at the sample data.
Instead, the SampleStream is given to a format-specific reader (which must match the stream's actual format):

```
reader, err := ffau.NewPackedS16Stream(resampled)

for {
    samples, err := reader.Read()
    // copy samples if required
}
```

`Err` will be `io.EOF` once the stream is exhausted.
The type of `samples` depends on the reader used. In this case it's a one-dimensional slice with channel data packed (interleaved) side by side.

The libavcodec docs suggest that some decoders will return zero length data packets (eg. if no samples have been decoded since the last packet?).
At this point ffau makes no attempt to handle this situation, so the caller to Read will recieve a zero-length slice - this should not be used as an end of stream indicator.

Once finished with the file, the SampleStream and FormatContext should be closed to free associated resources:

```
resampled.Close() // also calls Close on the underlying stream
fmtCtx.Close()
```
