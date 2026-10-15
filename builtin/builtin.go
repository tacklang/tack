package builtin

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Func is a builtin declared in std/builtin as an extern func: its name,
// its signature, and the C name its body is defined under. Builtins return
// nothing, so parameters are the whole of the signature.
type Func struct {
	Name string
	// CName is the C function a call to the builtin lowers to, as
	// extern("...") gives it in std/builtin. Its body is C, in a .c file
	// of std/builtin, which the driver compiles and links.
	CName  string
	Params []types.Type
}

// Set is the builtins of one std/builtin directory, as Load read them. It
// is never changed once loaded, so one Set can be shared by every stage of
// a compilation, and by compilations running at once. A nil *Set has no
// builtins: every method treats it as empty.
type Set struct {
	dir    string   // the directory loaded
	funcs  []Func   // every builtin, in file order and then declaration order
	cFiles []string // every .c file in dir, by name
}

// Dir returns the directory the builtins were loaded from.
func (s *Set) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Funcs returns every builtin, in the order the files of std/builtin, by
// name, and then their declarations list them, the same every time.
func (s *Set) Funcs() []Func {
	if s == nil {
		return nil
	}
	return s.funcs
}

// CFiles returns the path of every C file directly in the directory the
// builtins were loaded from, in the byte order of their names: the files
// that define the builtins' bodies, and whatever those call.
func (s *Set) CFiles() []string {
	if s == nil {
		return nil
	}
	return s.cFiles
}

// Lookup returns the builtin named name.
func (s *Set) Lookup(name string) (Func, bool) {
	for _, f := range s.Funcs() {
		if f.Name == name {
			return f, true
		}
	}
	return Func{}, false
}

// Names returns the name of every builtin, the declared ones in Funcs'
// order and then the generic ones. These are the names a call can bind to
// without a declaration.
func (s *Set) Names() []string {
	funcs := s.Funcs()
	names := make([]string, 0, len(funcs)+len(generic))
	for _, f := range funcs {
		names = append(names, f.Name)
	}
	return append(names, generic...)
}

// generic holds the builtins whose parameters no Tack type can describe,
// so they have no declaration and no Func: len takes any array, view,
// list or string; make takes a list type; append takes any list and any
// number of values of its element type, or a view of them spread; and
// reserve takes any list and a count. They are part of the compiler, not
// of std/builtin, and the type checker checks each of them itself.
var generic = []string{"len", "make", "append", "reserve"}

// ListSymbols holds the C functions of std/builtin that the generated C of
// a program with lists calls: the one that grows a list's buffer, through
// std's TACK_LIST_APPEND, TACK_LIST_APPEND_N and TACK_LIST_RESERVE, and
// the one that frees it. No builtin declares
// them, so a program that uses lists needs the std objects that define
// them linked whether or not it calls a builtin.
var ListSymbols = []string{"tack_list_grow_", "tack_list_free_"}

// UsesLists reports whether prog, which must have been type-checked, has
// a list type anywhere, whose values its generated C grows and frees
// through ListSymbols.
func UsesLists(prog *ast.Program) bool {
	table := prog.Types
	for i := range table.Count() {
		if table.IsList(types.Added(i)) {
			return true
		}
	}
	return false
}

// Generic returns the builtins the type checker checks itself, since their
// parameter accepts more than one type. They are the same whatever
// std/builtin declares.
func Generic() []string { return generic }
