package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// stdObject is a compiled C file of std/builtin and the symbols its object
// defines and leaves undefined.
type stdObject struct {
	cObject
	syms symbols
}

// linkStd compiles the C files of std/builtin into dir and returns the
// objects among them the program needs, in the order of their files'
// names: those defining a builtin the program calls and, in turn, those
// defining a symbol an object already needed leaves undefined, the
// members of an archive a linker would pull in. A symbol no std object
// defines, such as one of the C library's, is left to the linker. A
// program that calls no builtin needs none, and std's C is not compiled.
//
// Each file is compiled with the backend's CompileStdCmd at the build's
// level, and its object read as the program's are, so no tool beyond
// clang is needed: no ar, and no linker of its own. A builtin no std
// object defines, a std object defining main, and two defining one symbol
// are errors.
func (b *builder) linkStd(dir string) ([]cObject, error) {
	used := b.std.builtinSet().Used(b.res.Program)
	if len(used) == 0 {
		return nil, nil
	}
	objs, definer, err := b.compileStd(dir)
	if err != nil {
		return nil, err
	}

	need := make([]string, 0, len(used))
	for _, fn := range used {
		if _, ok := definer[fn.CName]; !ok {
			return nil, fmt.Errorf("builtin %s has no C definition: no C file in %s defines %s",
				fn.Name, b.std.builtins.Dir(), fn.CName)
		}
		need = append(need, fn.CName)
	}
	linked := make([]bool, len(objs))
	for len(need) > 0 {
		sym := need[len(need)-1]
		need = need[:len(need)-1]
		if i, ok := definer[sym]; ok && !linked[i] {
			linked[i] = true
			need = append(need, objs[i].syms.undefined...)
		}
	}

	var out []cObject
	for i, o := range objs {
		if linked[i] {
			out = append(out, o.cObject)
		}
	}
	return out, nil
}

// compileStd compiles every C file of std/builtin into dir, in name
// order, and returns their objects and, for each symbol one defines, the
// index of that object. An object defining main, and two defining one
// symbol, are errors, as for the program's C files.
func (b *builder) compileStd(dir string) ([]stdObject, map[string]int, error) {
	show := shower()
	files := b.std.builtins.CFiles()
	objs := make([]stdObject, len(files))
	definer := make(map[string]int)  // the index in objs of the object defining each symbol
	first := make(map[string]string) // the file first defining each symbol, for checkSymbol
	for i, f := range files {
		shown := show(f)
		obj := filepath.Join(dir, "std-"+strings.TrimSuffix(filepath.Base(f), ".c")+".o")
		cmd := b.backend.CompileStdCmd(f, b.std.includeDir(), obj, b.opt)
		if err := b.runTool(cmd, shown); err != nil {
			return nil, nil, err
		}
		syms, err := readSymbols(obj)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read what %s defines: %w", shown, err)
		}
		for _, sym := range syms.defined {
			if err := checkSymbol(sym, shown, first); err != nil {
				return nil, nil, err
			}
			definer[sym] = i
		}
		objs[i] = stdObject{cObject: cObject{shown: shown, obj: obj}, syms: syms}
	}
	return objs, definer, nil
}
