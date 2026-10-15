package main

import (
	"debug/elf"
	"debug/macho"
	"errors"
	"fmt"
	"strings"
)

// Mach-O symbol type bits, from <mach-o/nlist.h>, which debug/macho does
// not name.
const (
	machoStab = 0xe0 // N_STAB: a debugging entry, not a symbol
	machoType = 0x0e // N_TYPE: the bits that say where a symbol is
	machoUndf = 0x00 // N_UNDF: undefined, or common when its value is not 0
	machoAbs  = 0x02 // N_ABS: an absolute symbol, defined
	machoSect = 0x0e // N_SECT: defined in a section
	machoExt  = 0x01 // N_EXT: external, visible to other objects
)

// symbols is the external symbols of an object file, by C name.
type symbols struct {
	// defined is every symbol the file defines and exports to other
	// objects: a function or a variable that is not static.
	defined []string
	// undefined is every symbol the file uses and leaves for another
	// object, or a library, to define.
	undefined []string
}

// readSymbols returns the external symbols of the object file, or
// executable, at path. It reads Mach-O, the format clang writes on macOS,
// whose symbols carry a leading underscore, which is dropped, and ELF, the
// format on Linux. Reading the file directly, rather than running nm,
// keeps the build to clang alone.
func readSymbols(path string) (symbols, error) {
	if f, err := macho.Open(path); err == nil {
		defer f.Close() //nolint:errcheck // read-only; nothing to lose
		return machoSymbols(f), nil
	}
	f, err := elf.Open(path)
	if err != nil {
		return symbols{}, fmt.Errorf("not a Mach-O or ELF object file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only; nothing to lose
	syms, err := f.Symbols()
	if errors.Is(err, elf.ErrNoSymbols) {
		return symbols{}, nil
	}
	if err != nil {
		return symbols{}, fmt.Errorf("reading symbols: %w", err)
	}
	var out symbols
	for _, s := range syms {
		bind := elf.ST_BIND(s.Info)
		if bind != elf.STB_GLOBAL && bind != elf.STB_WEAK || s.Name == "" {
			continue
		}
		if s.Section == elf.SHN_UNDEF {
			out.undefined = append(out.undefined, s.Name)
		} else {
			out.defined = append(out.defined, s.Name)
		}
	}
	return out, nil
}

// machoSymbols returns the external symbols of f. A common symbol, an
// undefined one with a size as its value, which clang writes only under
// -fcommon, is a tentative definition, and is in neither list.
func machoSymbols(f *macho.File) symbols {
	var out symbols
	if f.Symtab == nil {
		return out
	}
	for _, s := range f.Symtab.Syms {
		if s.Type&machoStab != 0 || s.Type&machoExt == 0 {
			continue
		}
		name := strings.TrimPrefix(s.Name, "_")
		switch where := s.Type & machoType; {
		case where == machoSect || where == machoAbs:
			out.defined = append(out.defined, name)
		case where == machoUndf && s.Value == 0:
			out.undefined = append(out.undefined, name)
		}
	}
	return out
}
