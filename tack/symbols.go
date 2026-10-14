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
	machoAbs  = 0x02 // N_ABS: an absolute symbol, defined
	machoSect = 0x0e // N_SECT: defined in a section
	machoExt  = 0x01 // N_EXT: external, visible to other objects
)

// definedSymbols returns the C name of every symbol the object file at
// path defines and exports to other objects: a function or a variable that
// is not static. It reads Mach-O, the format clang writes on macOS, whose
// symbols carry a leading underscore, which is dropped, and ELF, the format
// on Linux. Reading the file directly, rather than running nm, keeps the
// build to clang alone.
func definedSymbols(path string) ([]string, error) {
	if f, err := macho.Open(path); err == nil {
		defer f.Close() //nolint:errcheck // read-only; nothing to lose
		return machoSymbols(f), nil
	}
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("not a Mach-O or ELF object file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only; nothing to lose
	syms, err := f.Symbols()
	if errors.Is(err, elf.ErrNoSymbols) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading symbols: %w", err)
	}
	var names []string
	for _, s := range syms {
		bind := elf.ST_BIND(s.Info)
		if (bind == elf.STB_GLOBAL || bind == elf.STB_WEAK) && s.Section != elf.SHN_UNDEF {
			names = append(names, s.Name)
		}
	}
	return names, nil
}

// machoSymbols returns the C name of every external symbol f defines.
func machoSymbols(f *macho.File) []string {
	if f.Symtab == nil {
		return nil
	}
	var names []string
	for _, s := range f.Symtab.Syms {
		where := s.Type & machoType
		if s.Type&machoStab != 0 || s.Type&machoExt == 0 ||
			where != machoSect && where != machoAbs {
			continue
		}
		names = append(names, strings.TrimPrefix(s.Name, "_"))
	}
	return names
}
