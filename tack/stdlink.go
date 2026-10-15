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
// program that calls no builtin needs none: std's C is not compiled, and
// the build cache is not looked for.
//
// Each file is compiled with the backend's CompileStdCmd at the build's
// level, or its object taken from the build cache when an earlier build
// compiled it from the same inputs (compileStd), and the object read as
// the program's are, so no tool beyond clang is needed: no ar, and no
// linker of its own. A builtin no std
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

// compileStd puts the object of every C file of std/builtin into dir, in
// name order, and returns them and, for each symbol one defines, the index
// of that object. Each object comes from the build cache when it holds
// one for the file as this build would compile it, and is compiled, and
// cached, otherwise (cachedStdObject). An object defining main, and two defining
// one symbol, are errors, as for the program's C files. A cache that
// cannot be used, or a toolchain that cannot be identified, ends the
// build.
func (b *builder) compileStd(dir string) ([]stdObject, map[string]int, error) {
	cache, err := openCache(b.env)
	if err != nil {
		return nil, nil, err
	}
	toolchain, err := b.env.toolVersion(b.ctx, b.backend.VersionCmd())
	if err != nil {
		return nil, nil, fmt.Errorf("cannot identify the toolchain: %w", err)
	}
	include, err := hashTree(b.std.includeDir())
	if err != nil {
		return nil, nil, err
	}

	show := shower()
	files := b.std.builtins.CFiles()
	objs := make([]stdObject, len(files))
	definer := make(map[string]int)  // the index in objs of the object defining each symbol
	first := make(map[string]string) // the file first defining each symbol, for checkSymbol
	for i, f := range files {
		shown := show(f)
		obj := filepath.Join(dir, "std-"+strings.TrimSuffix(filepath.Base(f), ".c")+".o")
		key, err := b.stdKey(f, toolchain, include)
		if err != nil {
			return nil, nil, err
		}
		syms, err := b.cachedStdObject(cache, key, f, shown, obj)
		if err != nil {
			return nil, nil, err
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

// cachedStdObject writes to obj the object of std's C file src, shown as shown,
// and returns its symbols. The object is copied from cache when it holds
// a sound one under key, one whose bytes hash to what the key says and
// whose symbols can be read. Otherwise src is compiled with the backend's
// CompileStdCmd at the build's level and the object cached under key,
// replacing whatever was there.
func (b *builder) cachedStdObject(cache *objCache, key, src, shown, obj string) (symbols, error) {
	hit, err := cache.get(key, obj)
	if err != nil {
		return symbols{}, err
	}
	if hit {
		if syms, readErr := readSymbols(obj); readErr == nil {
			return syms, nil
		}
		// Bytes that hash right but are no object were cached by
		// something other than tack; they are compiled again below.
	}
	if err = b.runTool(
		b.backend.CompileStdCmd(src, b.std.includeDir(), obj, b.opt),
		shown,
	); err != nil {
		return symbols{}, err
	}
	syms, err := readSymbols(obj)
	if err != nil {
		return symbols{}, fmt.Errorf("cannot read what %s defines: %w", shown, err)
	}
	if err = cache.put(key, obj); err != nil {
		return symbols{}, err
	}
	return syms, nil
}
