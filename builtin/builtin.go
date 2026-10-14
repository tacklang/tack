package builtin

import "github.com/tacklang/tack/types"

// Func is a builtin declared in builtin.tck as an extern func: its name, its
// signature, and the C name its body is defined under. Builtins return
// nothing today, so parameters are the whole of the signature.
type Func struct {
	Name string
	// CName is the C function a call to the builtin lowers to,
	// tack_builtin_ and its name, as extern("...") gives it in builtin.tck.
	// Its body is C, which the backend supplies for its target.
	CName  string
	Params []types.Type
}

var funcs = []Func{
	{Name: "print", Params: []types.Type{types.String}, CName: "tack_builtin_print"},
	{Name: "exit", Params: []types.Type{types.Int64}, CName: "tack_builtin_exit"},
}

// Funcs returns every builtin, in a fixed order.
func Funcs() []Func { return funcs }

// generic holds the builtins whose parameter no Tack type can describe, so
// they have no Func: len takes any array or a string. The type checker
// checks each of them itself.
var generic = []string{"len"}

// Names returns the name of every builtin, ordinary and generic, in a fixed
// order. These are the names a call can bind to without a declaration.
func Names() []string {
	names := make([]string, 0, len(funcs)+len(generic))
	for _, f := range funcs {
		names = append(names, f.Name)
	}
	return append(names, generic...)
}

// Generic returns the builtins the type checker checks itself, since their
// parameter accepts more than one type.
func Generic() []string { return generic }

// Lookup returns the builtin named name.
func Lookup(name string) (Func, bool) {
	for _, f := range funcs {
		if f.Name == name {
			return f, true
		}
	}
	return Func{}, false
}
