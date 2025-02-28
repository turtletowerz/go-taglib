package taglib

import (
	"fmt"
	"path/filepath"
)

var ErrReadonly = fmt.Errorf("attempted write on read-only file")

type File struct {
	readonly bool
	mod      *module
	path     string
}

// Opens a TagLib supported file
func Open(path string) (*File, error) {
	return open(path, false)
}

// Opens a TagLib supported file in read-only mode.
// Any write operations will return an error of type ErrReadonly
func OpenReadonly(path string) (*File, error) {
	return open(path, true)
}

func open(p string, readonly bool) (*File, error) {
	path, err := filepath.Abs(p)
	if err != nil {
		return nil, fmt.Errorf("make path abs %w", err)
	}

	mod, err := newModule(filepath.Dir(path), readonly)
	if err != nil {
		return nil, fmt.Errorf("init module: %w", err)
	}

	return &File{
		readonly: readonly,
		mod:      mod,
		path:     wasmPath(path),
	}, nil
}

func (f *File) Close() error {
	return f.mod.close()
}
