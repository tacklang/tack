package builtin

import "github.com/tacklang/tack/ast"

// Called returns the builtin of s that call calls, and whether it calls
// one: its callee is a name the resolver bound to a builtin, which it does
// with a nil *ast.Func, and that name is one of s's declared builtins. A
// generic builtin, such as len, is not one; it has no Func.
func (s *Set) Called(call *ast.Call) (Func, bool) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return Func{}, false
	}
	if fn, isFunc := id.Def.(*ast.Func); !isFunc || fn != nil {
		return Func{}, false
	}
	return s.Lookup(id.Name)
}

// Used returns every builtin of s that prog, which must have been
// resolved, calls anywhere, in Funcs' order, each once.
func (s *Set) Used(prog *ast.Program) []Func {
	called := make(map[string]bool)
	for _, f := range prog.Files() {
		ast.Walk(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.Call); ok {
				if fn, ok := s.Called(call); ok {
					called[fn.Name] = true
				}
			}
			return true
		})
	}
	var used []Func
	for _, fn := range s.Funcs() {
		if called[fn.Name] {
			used = append(used, fn)
		}
	}
	return used
}
