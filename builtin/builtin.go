package builtin

import "github.com/tacklang/tack/types"

// Func is a builtin's signature. Builtins return nothing today, so parameters
// are the whole of it.
type Func struct {
	Name   string
	Params []types.Type
}

var funcs = []Func{
	{Name: "print", Params: []types.Type{types.String}},
	{Name: "exit", Params: []types.Type{types.Int64}},
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
