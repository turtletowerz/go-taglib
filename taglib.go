package taglib

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:generate cmake -DCMAKE_BUILD_TYPE=Release -DCMAKE_EXPORT_COMPILE_COMMANDS=ON -DWITH_ZLIB=OFF -DBUILD_SHARED_LIBS=OFF -DBUILD_TESTING=OFF -DWASI_SDK_PREFIX=/opt/wasi-sdk -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -B build .
//go:generate cmake --build build --target taglib
//go:generate mv build/taglib.wasm .
//go:generate wasm-opt --strip -c -O3 taglib.wasm -o taglib.wasm

//go:embed taglib.wasm
var Binary []byte // WASM blob. To override, go build -ldflags="-X 'go.senan.xyz/taglib-wasm.BinaryPath=/path/to/taglib.wasm'"
var BinaryPath string

var ErrInvalidFile = fmt.Errorf("invalid file")
var ErrSavingFile = fmt.Errorf("can't save file")

func ReadTags(path string) (map[string][]string, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("make path abs %w", err)
	}

	dir := filepath.Dir(path)
	mod, err := newModuleRO(dir)
	if err != nil {
		return nil, fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var raw []string
	mod.call("taglib_file_tags", &raw, path)
	if raw == nil {
		return nil, ErrInvalidFile
	}

	var tags = map[string][]string{}
	for _, row := range raw {
		k, v, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		tags[k] = append(tags[k], v)
	}
	return tags, nil
}

type Properties struct {
	Length     time.Duration
	Channels   uint
	SampleRate uint
	Bitrate    uint
}

func ReadProperties(path string) (Properties, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return Properties{}, fmt.Errorf("make path abs %w", err)
	}

	dir := filepath.Dir(path)
	mod, err := newModuleRO(dir)
	if err != nil {
		return Properties{}, fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	const (
		audioPropertyLengthInMilliseconds = iota
		audioPropertyChannels
		audioPropertySampleRate
		audioPropertyBitrate
		audioPropertyLen
	)

	raw := make([]int, 0, audioPropertyLen)
	mod.call("taglib_file_audioproperties", &raw, path)

	return Properties{
		Length:     time.Duration(raw[audioPropertyLengthInMilliseconds]) * time.Millisecond,
		Channels:   uint(raw[audioPropertyChannels]),
		SampleRate: uint(raw[audioPropertySampleRate]),
		Bitrate:    uint(raw[audioPropertyBitrate]),
	}, nil
}

func WriteTags(path string, tags map[string][]string) error {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("make path abs %w", err)
	}

	dir := filepath.Dir(path)
	mod, err := newModule(dir)
	if err != nil {
		return fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var raw []string
	for k, vs := range tags {
		for _, v := range vs {
			raw = append(raw, k+"\t"+v)
		}
	}

	var out bool
	mod.call("taglib_file_write_tags", &out, path, raw)
	if !out {
		return ErrSavingFile
	}
	return nil
}

type rc struct {
	wazero.Runtime
	wazero.CompiledModule
}

var getRuntimeOnce = sync.OnceValues(func() (rc, error) {
	ctx := context.Background()

	cacheDir := filepath.Join(os.TempDir(), "go-taglib-wasm")
	compilationCache, err := wazero.NewCompilationCacheWithDir(cacheDir)
	if err != nil {
		return rc{}, err
	}

	runtime := wazero.NewRuntimeWithConfig(ctx,
		wazero.NewRuntimeConfig().
			WithCompilationCache(compilationCache),
	)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	_, err = runtime.
		NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func(int32) int32 { panic("__cxa_allocate_exception") }).Export("__cxa_allocate_exception").
		NewFunctionBuilder().WithFunc(func(int32, int32, int32) { panic("__cxa_throw") }).Export("__cxa_throw").
		Instantiate(ctx)
	if err != nil {
		return rc{}, err
	}

	var binary = Binary
	if BinaryPath != "" {
		binary, err = os.ReadFile(BinaryPath)
		if err != nil {
			return rc{}, fmt.Errorf("read custom binary path: %w", err)
		}
		clear(Binary)
	}

	compiled, err := runtime.CompileModule(ctx, binary)
	if err != nil {
		return rc{}, err
	}

	return rc{
		Runtime:        runtime,
		CompiledModule: compiled,
	}, nil
})

type module struct {
	mod api.Module
}

func newModule(dir string) (module, error)   { return newModuleOpt(dir, false) }
func newModuleRO(dir string) (module, error) { return newModuleOpt(dir, true) }
func newModuleOpt(dir string, readOnly bool) (module, error) {
	rt, err := getRuntimeOnce()
	if err != nil {
		return module{}, fmt.Errorf("get runtime once: %w", err)
	}

	fsConfig := wazero.NewFSConfig()
	if readOnly {
		fsConfig = fsConfig.WithReadOnlyDirMount(dir, dir)
	} else {
		fsConfig = fsConfig.WithDirMount(dir, dir)
	}

	cfg := wazero.
		NewModuleConfig().
		WithName("").
		WithFSConfig(fsConfig)

	ctx := context.Background()
	mod, err := rt.Runtime.InstantiateModule(ctx, rt.CompiledModule, cfg)
	if err != nil {
		return module{}, err
	}

	if _, err := mod.ExportedFunction("_initialize").Call(ctx); err != nil {
		return module{}, fmt.Errorf("_initialize: %w", err)
	}

	return module{
		mod: mod,
	}, nil
}

func (m *module) malloc(size uint32) uint32 {
	var ptr uint32
	m.call("malloc", &ptr, size)
	if ptr == 0 {
		panic("no ptr")
	}
	return ptr
}

func (m *module) call(name string, dest any, args ...any) {
	params := make([]uint64, 0, len(args))
	for _, a := range args {
		switch a := a.(type) {
		case int:
			params = append(params, uint64(a))
		case uint32:
			params = append(params, uint64(a))
		case uint64:
			params = append(params, a)
		case string:
			params = append(params, uint64(makeString(m, a)))
		case []string:
			params = append(params, uint64(makeStrings(m, a)))
		default:
			panic(fmt.Sprintf("unknown argument type %T", a))
		}
	}

	results, err := m.mod.ExportedFunction(name).Call(context.Background(), params...)
	if err != nil {
		panic(err)
	}
	if len(results) == 0 {
		return
	}
	result := results[0]

	switch dest := dest.(type) {
	case *int:
		*dest = int(result)
	case *uint32:
		*dest = uint32(result)
	case *uint64:
		*dest = uint64(result)
	case *bool:
		*dest = result == 1
	case *string:
		if result != 0 {
			*dest = readString(m, uint32(result))
		}
	case *[]string:
		if result != 0 {
			*dest = readStrings(m, uint32(result))
		}
	case *[]int:
		if result != 0 {
			*dest = readInts(m, uint32(result), cap(*dest))
		}
	default:
		panic(fmt.Sprintf("unknown result type %T", dest))
	}
}

func (m *module) close() {
	if err := m.mod.Close(context.Background()); err != nil {
		panic(err)
	}
}

func makeString(m *module, s string) uint32 {
	b := append([]byte(s), 0)
	ptr := m.malloc(uint32(len(b)))
	if !m.mod.Memory().Write(ptr, b) {
		panic("failed to write to mod.module.Memory()")
	}
	return ptr
}

func makeStrings(m *module, s []string) uint32 {
	arrayPtr := m.malloc(uint32((len(s) + 1) * 4))
	for i, s := range s {
		b := append([]byte(s), 0)
		ptr := m.malloc(uint32(len(b)))
		if !m.mod.Memory().Write(ptr, b) {
			panic("failed to write to mod.module.Memory()")
		}
		if !m.mod.Memory().WriteUint32Le(arrayPtr+uint32(i*4), ptr) {
			panic("failed to write pointer to mod.module.Memory()")
		}
	}
	if !m.mod.Memory().WriteUint32Le(arrayPtr+uint32(len(s)*4), 0) {
		panic("failed to write pointer to memory")
	}
	return arrayPtr
}

func readString(m *module, ptr uint32) string {
	size := uint32(64)
	buf, ok := m.mod.Memory().Read(ptr, size)
	if !ok {
		panic("memory error")
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		return string(buf[:i])
	}
	for {
		next, ok := m.mod.Memory().Read(ptr+size, size)
		if !ok {
			panic("memory error")
		}
		if i := bytes.IndexByte(next, 0); i >= 0 {
			return string(append(buf, next[:i]...))
		}
		buf = append(buf, next...)
		size += size
	}
}

func readStrings(m *module, ptr uint32) []string {
	strs := []string{} // non nil so call knows if it's just empty
	for {
		stringPtr, ok := m.mod.Memory().ReadUint32Le(ptr)
		if !ok {
			panic("memory error")
		}
		if stringPtr == 0 {
			break
		}
		str := readString(m, stringPtr)
		strs = append(strs, str)
		ptr += 4
	}
	return strs
}

func readInts(m *module, ptr uint32, len int) []int {
	ints := make([]int, 0, len)
	for i := range len {
		i, ok := m.mod.Memory().ReadUint32Le(ptr + uint32(4*i))
		if !ok {
			panic("memory error")
		}
		ints = append(ints, int(i))
	}
	return ints
}
