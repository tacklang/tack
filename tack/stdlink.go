package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tacklang/tack/builtin"
)

// runtimeShown is the directory messages name the runtime's C files in:
// they are built into the compiler, and the path each build writes them
// to is a temporary one.
const runtimeShown = "<runtime>"

// stdObject is a compiled C file of the runtime and the symbols its object
// defines and leaves undefined.
type stdObject struct {
	cObject
	syms symbols
}

// linkStd compiles the C files of the runtime built into the compiler,
// which writeRuntime wrote out, into dir and returns the
// objects among them the program needs, in the order of their files'
// names: those defining a builtin the program calls, the one defining
// the process's start and end when the program runs C beyond its
// generated C, the one defining
// the abort of an unreachable when the program holds one, those defining the
// list functions its generated C calls when it uses lists
// (builtin.ListSymbols), those defining a symbol starting tack_ that one
// of progObjs, the objects of the program's own C files, leaves undefined,
// as a C file calling std's list macros does, and, in turn, those defining
// a symbol an object already needed leaves undefined, the members of an
// archive a linker would pull in. A symbol no std object defines, such as
// one of the C library's, is left to the linker. A program that needs
// none of these needs no std object: std's C is not compiled, and the
// build cache is not looked for.
//
// Each file is compiled with the backend's CompileStdCmd at the build's
// level, or its object taken from the build cache when an earlier build
// compiled it from the same inputs (compileStd), and the object read as
// the program's are, so no tool beyond clang is needed: no ar, and no
// linker of its own. A builtin no std
// object defines, a std object defining main, and two defining one symbol
// are errors.
func (b *builder) linkStd(dir string, progObjs []cObject) ([]cObject, error) {
	used := b.std.builtinSet().Used(b.res.Program)
	runsC := b.std.builtinSet().RunsC(b.res.Program)
	lists := builtin.UsesLists(b.res.Program)
	unreachable := builtin.UsesUnreachable(b.res.Program)
	progNeed, err := stdSymbolsNeeded(progObjs)
	if err != nil {
		return nil, err
	}
	if len(used) == 0 && !runsC && !lists && !unreachable && len(progNeed) == 0 {
		return nil, nil
	}
	objs, definer, err := b.compileStd(dir)
	if err != nil {
		return nil, err
	}
	need, err := b.stdRoots(used, runsC, lists, unreachable, definer)
	if err != nil {
		return nil, err
	}
	return membersFor(append(need, progNeed...), objs, definer), nil
}

// stdRoots returns the symbols a program needs std to define whatever
// else it calls: the C name of each builtin of used, those it calls,
// when it runs C beyond its generated C (builtin.Set.RunsC), the start
// and end its generated main calls (builtin.StartSymbol and
// builtin.EndSymbol), when it holds an unreachable, the abort its
// generated C calls (builtin.UnreachableSymbol), and, when it has lists,
// the list functions its generated C calls. A builtin, the start or end,
// the abort, or a list function, that no std object defines, as definer
// says, is an error.
func (b *builder) stdRoots(
	used []builtin.Func,
	runsC, lists, unreachable bool,
	definer map[string]int,
) ([]string, error) {
	need := make([]string, 0, len(used)+len(builtin.ListSymbols)+3)
	for _, fn := range used {
		if _, ok := definer[fn.CName]; !ok {
			return nil, fmt.Errorf("builtin %s has no C definition: the runtime defines no %s",
				fn.Name, fn.CName)
		}
		need = append(need, fn.CName)
	}
	if runsC {
		for _, sym := range []string{builtin.StartSymbol, builtin.EndSymbol} {
			if _, ok := definer[sym]; !ok {
				return nil, fmt.Errorf("the program's start and end have no C definition: the runtime defines no %s", sym)
			}
			need = append(need, sym)
		}
	}
	if unreachable {
		if _, ok := definer[builtin.UnreachableSymbol]; !ok {
			return nil, fmt.Errorf("unreachable has no C definition: the runtime defines no %s",
				builtin.UnreachableSymbol)
		}
		need = append(need, builtin.UnreachableSymbol)
	}
	if !lists {
		return need, nil
	}
	for _, sym := range builtin.ListSymbols {
		if _, ok := definer[sym]; !ok {
			return nil, fmt.Errorf("lists have no C definition: the runtime defines no %s", sym)
		}
		need = append(need, sym)
	}
	return need, nil
}

// membersFor returns the objects of objs, in order, that define a symbol
// of need, as definer says, or a symbol one already chosen leaves
// undefined, in turn: the members of an archive a linker would pull in.
func membersFor(need []string, objs []stdObject, definer map[string]int) []cObject {
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
	return out
}

// stdSymbolsNeeded returns every symbol starting tack_ that one of objs,
// the program's own C objects, leaves undefined, which only std can
// define, since no extern func of a program may take such a name. Those
// std does not define are left to the linker, which reports them.
func stdSymbolsNeeded(objs []cObject) ([]string, error) {
	var need []string
	for _, o := range objs {
		syms, err := readSymbols(o.obj)
		if err != nil {
			return nil, fmt.Errorf("cannot read what %s needs: %w", o.shown, err)
		}
		for _, sym := range syms.undefined {
			if strings.HasPrefix(sym, "tack_") {
				need = append(need, sym)
			}
		}
	}
	return need, nil
}

// compileStd puts the object of every C file of the runtime into dir, in
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
	files := builtin.RuntimeCFiles()
	objs := make([]stdObject, len(files))
	definer := make(map[string]int)  // the index in objs of the object defining each symbol
	first := make(map[string]string) // the file first defining each symbol, for checkSymbol
	for i, f := range files {
		shown := runtimeShown + "/" + f.Name
		obj := filepath.Join(dir, "std-"+strings.TrimSuffix(f.Name, ".c")+".o")
		syms, err := b.cachedStdObject(cache, b.stdKey(f, toolchain), b.runtimeCFile(f.Name),
			shown, obj)
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

// cachedStdObject writes to obj the object of the runtime's C file src,
// shown as shown,
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
		b.backend.CompileStdCmd(src, b.includeDir(), obj, b.opt),
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
