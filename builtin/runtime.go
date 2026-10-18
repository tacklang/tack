package builtin

import (
	"embed"
	"io/fs"
	"path"
	"sort"
)

// runtimeFS holds the runtime built into the compiler: the C files of the
// builtins and of lists, and the header every C file of a program and of
// std includes as <tack/tack.h>.
//
//go:embed runtime/*.c runtime/tack/tack.h
var runtimeFS embed.FS

// HeaderPath is where the runtime header is found below an include
// directory: C includes it as <tack/tack.h>.
const HeaderPath = "tack/tack.h"

// RuntimeFile is a file of the runtime built into the compiler: its base
// name and its bytes, which callers must not change.
type RuntimeFile struct {
	Name string
	Data []byte
}

// RuntimeCFiles returns the runtime's C files, in the byte order of their
// names: exit.c, list.c and print.c, the bodies of the builtins with a C
// body and of lists. The driver writes them where it compiles them and
// links the objects a program needs.
func RuntimeCFiles() []RuntimeFile {
	names, err := fs.Glob(runtimeFS, "runtime/*.c")
	if err != nil {
		panic("builtin: the embedded runtime cannot be listed: " + err.Error())
	}
	sort.Strings(names)
	files := make([]RuntimeFile, len(names))
	for i, name := range names {
		files[i] = RuntimeFile{Name: path.Base(name), Data: mustRead(name)}
	}
	return files
}

// RuntimeHeader returns the runtime header, <tack/tack.h>.
func RuntimeHeader() []byte { return mustRead("runtime/" + HeaderPath) }

// mustRead returns the embedded file at name, which go:embed guarantees
// is there.
func mustRead(name string) []byte {
	data, err := runtimeFS.ReadFile(name)
	if err != nil {
		panic("builtin: the embedded runtime has no " + name + ": " + err.Error())
	}
	return data
}
