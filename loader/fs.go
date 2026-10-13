package loader

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FS is what the loader reads files and directories through. Names are
// slash-separated paths; the OS file system takes absolute and relative
// ones alike, an fs.FS adapted by FromFS only the relative ones fs.FS
// allows.
type FS interface {
	// ReadFile returns the contents of the file name.
	ReadFile(name string) ([]byte, error)
	// ReadDir returns the entries of the directory name, sorted by name.
	ReadDir(name string) ([]fs.DirEntry, error)
}

// OS is the host's file system.
type OS struct{}

// ReadFile reads the file name from the host's file system.
func (OS) ReadFile(name string) ([]byte, error) {
	// Reading the files the user named is the point of a compiler.
	data, err := os.ReadFile(filepath.FromSlash(name)) //nolint:gosec // G304: by design
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return data, nil
}

// ReadDir reads the directory name from the host's file system.
func (OS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(filepath.FromSlash(name))
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return entries, nil
}

// FromFS returns an FS that reads fsys, as tests do from an in-memory
// file system.
func FromFS(fsys fs.FS) FS { return ioFS{fsys} }

// ioFS adapts an fs.FS to FS.
type ioFS struct{ fsys fs.FS }

// ReadFile reads the file name from the fs.FS.
func (f ioFS) ReadFile(name string) ([]byte, error) {
	data, err := fs.ReadFile(f.fsys, name)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return data, nil
}

// ReadDir reads the directory name from the fs.FS.
func (f ioFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.fsys, name)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return entries, nil
}
