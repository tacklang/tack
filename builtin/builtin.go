package builtin

import "github.com/tacklang/tack/types"

// Func is a builtin's signature. Builtins return nothing today, so parameters
// are the whole of it.
type Func struct {
	Name   string
	Params []types.Kind
}

var funcs = []Func{
	{Name: "print", Params: []types.Kind{types.String}},
	{Name: "exit", Params: []types.Kind{types.Int}},
}

// Funcs returns every builtin, in a fixed order.
func Funcs() []Func { return funcs }

// Lookup returns the builtin named name.
func Lookup(name string) (Func, bool) {
	for _, f := range funcs {
		if f.Name == name {
			return f, true
		}
	}
	return Func{}, false
}
