package builtin

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/types"
)

// Func is a builtin whose body is C, in the runtime built into the
// compiler (see RuntimeCFiles): its name, its parameters' names and
// types, and the C name its body is defined under. These builtins return
// nothing, so parameters are the whole of the signature.
type Func struct {
	Name string
	// CName is the C function a call to the builtin lowers to, defined in
	// one of the runtime's C files, which the driver compiles and links.
	CName      string
	Params     []types.Type
	ParamNames []string
}

// Set is the builtins a compilation uses: those Default returns, which
// are the same for every compilation. It is never changed, so one Set is
// shared by every stage and every compilation. A nil *Set has no
// builtins, as a dump of the tokens or the trees needs none: every method
// treats it as empty.
type Set struct {
	funcs []Func // every builtin with a C body, in the order Funcs lists them
}

// defaultSet is the one Set the compiler has, which Default returns.
var defaultSet = &Set{funcs: []Func{
	{
		Name:       "print",
		CName:      "tack_builtin_print",
		Params:     []types.Type{types.String},
		ParamNames: []string{"s"},
	},
	{
		Name:       "exit",
		CName:      "tack_builtin_exit",
		Params:     []types.Type{types.Int32},
		ParamNames: []string{"code"},
	},
}}

// Default returns the builtins the compiler implements. Nothing outside
// the compiler, no file a user or std holds, adds to them or changes them.
func Default() *Set { return defaultSet }

// Funcs returns every builtin whose body is C, print and then exit, the
// same every time.
func (s *Set) Funcs() []Func {
	if s == nil {
		return nil
	}
	return s.funcs
}

// Lookup returns the builtin with a C body named name.
func (s *Set) Lookup(name string) (Func, bool) {
	for _, f := range s.Funcs() {
		if f.Name == name {
			return f, true
		}
	}
	return Func{}, false
}

// Names returns the name of every builtin function, those with a C body
// in Funcs' order and then the generic ones. These are the names a call
// can bind to without a declaration.
func (s *Set) Names() []string {
	funcs := s.Funcs()
	names := make([]string, 0, len(funcs)+len(generic))
	for _, f := range funcs {
		names = append(names, f.Name)
	}
	for _, g := range generic {
		names = append(names, g.name)
	}
	return names
}

// genericFunc is a builtin the type checker checks itself, since a
// parameter of it accepts more than one type: its name and its
// parameters' names, as the builtins' file documents them.
type genericFunc struct {
	name   string
	params []string
}

// generic holds the builtins whose parameters no Tack type can describe:
// len takes any array, view, list, arena, pool, map or string; make
// takes a list, a map, an arena or a pool type; append takes any list and
// any number of values of its element type, or a view of them spread;
// reserve takes any list or arena and a count; pop takes any list; remove
// takes any list and an index, or any pool and a handle; swap takes any
// list and two indexes; replace takes a place of any type and a value of
// it; put takes any map, a key and a value; delete takes any map and a
// key; has takes any map and a key, or any pool and a handle; drop takes,
// as own, a variable of any type that owns something; add takes any arena
// and a value of its element type; add_all takes any arena and any number
// of values of its element type, or a view of them spread; and insert
// takes any pool and a value of its element type. The type checker checks
// each of them itself, and the C backend lowers each in place.
var generic = []genericFunc{
	{"len", []string{"v"}},
	{"make", []string{"t", "alloc"}},
	{"append", []string{"l", "elems"}},
	{"reserve", []string{"l", "n"}},
	{"pop", []string{"l"}},
	{"remove", []string{"l", "i"}},
	{"swap", []string{"l", "i", "j"}},
	{"replace", []string{"place", "new"}},
	{"put", []string{"m", "k", "v"}},
	{"delete", []string{"m", "k"}},
	{"has", []string{"m", "k"}},
	{"drop", []string{"x"}},
	{"add", []string{"a", "v"}},
	{"add_all", []string{"a", "elems"}},
	{"insert", []string{"p", "v"}},
}

// Generic returns the names of the builtins the type checker checks
// itself, since a parameter of each accepts more than one type.
func Generic() []string {
	names := make([]string, len(generic))
	for i, g := range generic {
		names[i] = g.name
	}
	return names
}

// ParamNames returns the names of the parameters of the builtin function
// named name, as the builtins' file declares them, and whether there is
// one.
func ParamNames(name string) ([]string, bool) {
	if fn, ok := defaultSet.Lookup(name); ok {
		return fn.ParamNames, true
	}
	for _, g := range generic {
		if g.name == name {
			return g.params, true
		}
	}
	return nil, false
}

// Values returns the names of the builtin values, true, false, noerror
// and c_allocator: keywords, which the lexer reads and the parser takes
// as literals, so no declaration can take them.
func Values() []string {
	return []string{
		lexer.True.String(), lexer.False.String(), lexer.NoError.String(),
		lexer.CAllocator.String(),
	}
}

// Types returns the names of the builtin types, every type a program
// names without declaring it: the basic types, then id, span and handle,
// written with an element type.
func Types() []string { return append(types.Names(), types.RefNames()...) }

// ListSymbols holds the C functions of the runtime that the generated C
// of a program with lists calls: the one that grows a list's buffer,
// through tack.h's TACK_LIST_APPEND, TACK_LIST_APPEND_N and
// TACK_LIST_RESERVE, the one that frees it, the one that says whether
// an arena, whose elements are a list's, may grow, and those that give a
// pool its tag, say whether it may grow and give the generation its new
// slot starts at. No builtin declares them, so a program that uses lists
// needs the runtime objects that define them linked whether or not it
// calls a builtin.
var ListSymbols = []string{
	"tack_list_grow_", "tack_list_free_", "tack_arena_room_",
	"tack_pool_tag_", "tack_pool_room_", "tack_pool_gen0_",
}

// UnreachableSymbol is the C function of the runtime that the abort an
// unreachable statement lowers to calls, through tack.h's
// TACK_UNREACHABLE. No builtin declares it, so a program holding an
// unreachable needs the runtime object that defines it linked whether or
// not it calls a builtin.
const UnreachableSymbol = "tack_unreachable_"

// UsesUnreachable reports whether prog holds an unreachable statement
// anywhere, whose abort its generated C calls through UnreachableSymbol.
func UsesUnreachable(prog *ast.Program) bool {
	found := false
	for _, f := range prog.Files() {
		ast.Walk(f, func(n ast.Node) bool {
			_, ok := n.(*ast.Unreachable)
			found = found || ok
			return !found
		})
	}
	return found
}

// UsesLists reports whether prog, which must have been type-checked, has
// a list, an arena, a pool or a map type anywhere, whose values its
// generated C grows and frees through ListSymbols: an arena's elements
// are a list's, and a pool's slots and a map's entries and slots are
// buffers as a list's is.
func UsesLists(prog *ast.Program) bool {
	table := prog.Types
	for i := range table.Count() {
		if typ := types.Added(i); table.IsList(typ) || table.IsMap(typ) || table.IsPool(typ) {
			return true
		}
	}
	return false
}
