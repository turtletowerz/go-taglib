package taglib

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:generate cmake -DWASI_SDK_PREFIX=/opt/wasi-sdk -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -B build .
//go:generate cmake --build build --target taglib
//go:generate mv build/taglib.wasm .
//go:generate wasm-opt --strip -c -O3 taglib.wasm -o taglib.wasm

//go:embed taglib.wasm
var binary []byte // WASM blob. To override, go build -ldflags="-X 'go.senan.xyz/taglib.binaryPath=/path/to/taglib.wasm'"
var binaryPath string

// WASI uses POSIXy paths, even on Windows
func wasmPath(p string) string {
	return filepath.ToSlash(p)
}

type rc struct {
	wazero.Runtime
	wazero.CompiledModule
}

var getRuntimeOnce = sync.OnceValues(func() (*rc, error) {
	ctx := context.Background()

	cacheDir, err := os.MkdirTemp("", "go-taglib-wasm")
	if err != nil {
		return nil, err
	}

	compilationCache, err := wazero.NewCompilationCacheWithDir(cacheDir)
	if err != nil {
		return nil, err
	}

	runtime := wazero.NewRuntimeWithConfig(ctx,
		wazero.NewRuntimeConfig().
			WithCompilationCache(compilationCache),
	)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	// TODO: Find some way to remove panics
	_, err = runtime.
		NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func(int32) int32 { panic("__cxa_allocate_exception") }).Export("__cxa_allocate_exception").
		NewFunctionBuilder().WithFunc(func(int32, int32, int32) { panic("__cxa_throw") }).Export("__cxa_throw").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	var bin = binary
	if binaryPath != "" {
		bin, err = os.ReadFile(binaryPath)
		if err != nil {
			return nil, fmt.Errorf("read custom binary path: %w", err)
		}
		clear(binary)
	}

	compiled, err := runtime.CompileModule(ctx, bin)
	if err != nil {
		return nil, err
	}

	return &rc{
		Runtime:        runtime,
		CompiledModule: compiled,
	}, nil
})

type module struct {
	mod api.Module
}

func newModule(dir string, readOnly bool) (*module, error) {
	rt, err := getRuntimeOnce()
	if err != nil {
		return nil, fmt.Errorf("get runtime once: %w", err)
	}

	fsConfig := wazero.NewFSConfig()
	if readOnly {
		fsConfig = fsConfig.WithReadOnlyDirMount(dir, wasmPath(dir))
	} else {
		fsConfig = fsConfig.WithDirMount(dir, wasmPath(dir))
	}

	cfg := wazero.
		NewModuleConfig().
		WithName("").
		WithStartFunctions("_initialize").
		WithFSConfig(fsConfig)

	mod, err := rt.InstantiateModule(context.Background(), rt.CompiledModule, cfg)
	if err != nil {
		return nil, err
	}

	return &module{
		mod: mod,
	}, nil
}

func (m *module) malloc(size uint32) (uint32, error) {
	var ptr uint32
	if err := m.call("malloc", &ptr, size); err != nil {
		return 0, fmt.Errorf("malloc error: %w", err)
	}
	if ptr == 0 {
		return 0, fmt.Errorf("no pointer returned")
	}
	return ptr, nil
}

func (m *module) call(name string, dest any, args ...any) error {
	params := make([]uint64, 0, len(args))
	for _, a := range args {
		switch a := a.(type) {
		case bool:
			if a {
				params = append(params, 1)
			} else {
				params = append(params, 0)
			}
		case int:
			params = append(params, uint64(a))
		case uint8:
			params = append(params, uint64(a))
		case uint32:
			params = append(params, uint64(a))
		case uint64:
			params = append(params, a)
		case string:
			s, err := m.makeString(a)
			if err != nil {
				return fmt.Errorf("making argument string: %w", err)
			}
			params = append(params, uint64(s))
		case []string:
			s, err := m.makeStrings(a)
			if err != nil {
				return fmt.Errorf("making argument strings: %w", err)
			}
			params = append(params, uint64(s))
		default:
			return fmt.Errorf("unknown argument type %T", a)
		}
	}

	results, err := m.mod.ExportedFunction(name).Call(context.Background(), params...)
	if err != nil {
		return fmt.Errorf("call %q: %w", name, err)
	}
	if len(results) == 0 {
		return nil
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
			*dest, err = m.readString(uint32(result))
		}
	case *[]string:
		if result != 0 {
			*dest, err = m.readStrings(uint32(result))
		}
	case *[]int:
		if result != 0 {
			*dest, err = m.readInts(uint32(result), cap(*dest))
		}
	default:
		return fmt.Errorf("unknown result type %T", dest)
	}

	if err != nil {
		return fmt.Errorf("reading destination type %T: %w", dest, err)
	}
	return nil
}

func (m *module) close() error {
	return m.mod.Close(context.Background())
}

func (m *module) makeString(s string) (uint32, error) {
	b := append([]byte(s), 0)
	ptr, err := m.malloc(uint32(len(b)))
	if err != nil {
		return 0, fmt.Errorf("malloc error: %w", err)
	}
	if !m.mod.Memory().Write(ptr, b) {
		return 0, fmt.Errorf("failed to write to mod.module.Memory()")
	}
	return ptr, nil
}

func (m *module) makeStrings(s []string) (uint32, error) {
	arrayPtr, err := m.malloc(uint32((len(s) + 1) * 4))
	if err != nil {
		return 0, fmt.Errorf("malloc error: %w", err)
	}

	for i, s := range s {
		b := append([]byte(s), 0)
		ptr, err := m.malloc(uint32(len(b)))
		if err != nil {
			return 0, fmt.Errorf("malloc error: %w", err)
		}
		if !m.mod.Memory().Write(ptr, b) {
			return 0, fmt.Errorf("failed to write to mod.module.Memory()")
		}
		if !m.mod.Memory().WriteUint32Le(arrayPtr+uint32(i*4), ptr) {
			return 0, fmt.Errorf("failed to write pointer to mod.module.Memory()")
		}
	}
	if !m.mod.Memory().WriteUint32Le(arrayPtr+uint32(len(s)*4), 0) {
		return 0, fmt.Errorf("failed to write pointer to memory")
	}
	return arrayPtr, nil
}

func (m *module) readString(ptr uint32) (string, error) {
	size := uint32(64)
	buf, ok := m.mod.Memory().Read(ptr, size)
	if !ok {
		return "", fmt.Errorf("memory error while reading string")
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		return string(buf[:i]), nil
	}
	for {
		next, ok := m.mod.Memory().Read(ptr+size, size)
		if !ok {
			return "", fmt.Errorf("memory error while reading byte")
		}
		if i := bytes.IndexByte(next, 0); i >= 0 {
			return string(append(buf, next[:i]...)), nil
		}
		buf = append(buf, next...)
		size += size
	}
}

func (m *module) readStrings(ptr uint32) ([]string, error) {
	strs := []string{} // non nil so call knows if it's just empty
	for {
		stringPtr, ok := m.mod.Memory().ReadUint32Le(ptr)
		if !ok {
			return nil, fmt.Errorf("memory error while reading string length")
		}
		if stringPtr == 0 {
			break
		}
		str, err := m.readString(stringPtr)
		if err != nil {
			return nil, fmt.Errorf("reading string from array: %w", err)
		}
		strs = append(strs, str)
		ptr += 4
	}
	return strs, nil
}

func (m *module) readInts(ptr uint32, len int) ([]int, error) {
	ints := make([]int, 0, len)
	for i := range len {
		i, ok := m.mod.Memory().ReadUint32Le(ptr + uint32(4*i))
		if !ok {
			return nil, fmt.Errorf("memory error while reading uint32")
		}
		ints = append(ints, int(i))
	}
	return ints, nil
}
