# go-taglib

This project is a Go library for reading and writing audio metadata tags. It provides a portable solution with no external dependencies required, thanks to an embedded WASM binary.

## Features

- **Read** and **write** metadata tags for audio files, including support for multi-valued tags.
- Retrieve audio properties such as length, bitrate, sample rate, and channels.
- Supports multiple audio formats including _MP3_, _FLAC_, _M4A_, _WAV_, _OGG_, _WMA_, and more.
- Safe for concurrent use
- [Fast](#performance)

## Usage

### Reading Tags

```go
import "go.senan.xyz/taglib"

tags, err := taglib.ReadTags("path/to/audiofile.mp3")
// check(err)
fmt.Println(tags)
```

### Writing Tags

```go
import "go.senan.xyz/taglib"

err := taglib.WriteTags("path/to/audiofile.mp3", map[string][]string{
    "ALBUMARTISTS":        {"David Bynre", "Brian Eno"},
    "ALBUMARTISTS_CREDIT": {"Brian Eno & David Bynre"},
    "ALBUM":               {"My Life in the Bush of Ghosts"},
    "TRACKNUMBER":         {"1"},
})
```

### Reading Audio Properties

```go
import "go.senan.xyz/taglib"

properties, err := taglib.ReadProperties("path/to/audiofile.mp3")
// check(err)
fmt.Printf("Length: %v, Bitrate: %d, SampleRate: %d, Channels: %d\n", properties.Length, properties.Bitrate, properties.SampleRate, properties.Channels)
```

## Manually Building and Using the WASM Binary

The binary is already included in the package. However if you want to manually build and override it, you can with WASI SDK and Go build flags

1. Install [WASI SDK](https://github.com/WebAssembly/wasi-sdk) globally. The default installation path is `/opt/wasi-sdk/`
2. Clone this repository and Git submodules

   ```console
   $ git clone "https://github.com/sentriz/go-taglib.git" --recursive
   $ cd go-taglib
   ```

3. Generate the WASM binary:

   ```console
   $ go generate ./...
   $ # taglib.wasm created
   ```

4. Use the new binary in your project

   ```console
   $ GCO_ENABLED=0 go build -ldflags="-X 'go.senan.xyz/taglib.binaryPath=/path/to/taglib.wasm'" ./your/project/...
   ```

### Performance

In this example, tracks are read on average in `0.3 ms`, and written in `1.85 ms`

```
goos: linux
goarch: amd64
pkg: go.senan.xyz/taglib
cpu: AMD Ryzen 7 7840U w/ Radeon  780M Graphics
BenchmarkWrite-16         608   1847873 ns/op
BenchmarkRead-16         3802    299247 ns/op
```

## License

This project is licensed under the GNU Lesser General Public License v2.1. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Acknowledgments

- [TagLib](https://taglib.org/) for the audio metadata library.
- [Wazero](https://github.com/tetratelabs/wazero) for the WebAssembly runtime in Go.
