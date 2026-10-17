package c

import "github.com/tacklang/tack/types"

// The C names of the types a program uses, and of the helpers each has,
// are made from the type's ID. In an instance of a generic function, a
// type parameter, and a type made of one, list[T] or map[string]T, stands
// for the type made the same way of its type arguments (see
// types.Table.Bound), whose ID is not its own, so each name below is made
// from the type its argument stands for, and the instance's list[T] is
// the very C type its caller's list[int64] is.

// listName is the name of the C struct of the list type typ stands for.
func (g *generator) listName(typ types.Type) string {
	return listNameOf(g.prog.Types.Bound(typ))
}

// mapName is the name of the C struct of the map type typ stands for.
func (g *generator) mapName(typ types.Type) string {
	return mapNameOf(g.prog.Types.Bound(typ))
}

// entryName is the name of the C struct of one entry of the map type typ
// stands for.
func (g *generator) entryName(typ types.Type) string {
	return entryNameOf(g.prog.Types.Bound(typ))
}

// hashName is the name of the hash helper of the key type typ stands for.
func (g *generator) hashName(typ types.Type) string {
	return hashNameOf(g.prog.Types.Bound(typ))
}

// mapHelper is the name of the helper what of the map type typ stands for.
func (g *generator) mapHelper(what string, typ types.Type) string {
	return mapHelperOf(what, g.prog.Types.Bound(typ))
}

// viewName is the name of the C struct of the view type typ stands for.
func (g *generator) viewName(typ types.Type) string {
	return viewNameOf(g.prog.Types.Bound(typ))
}

// arrayName is the name of the C struct of the array type typ stands for.
func (g *generator) arrayName(typ types.Type) string {
	return arrayNameOf(g.prog.Types.Bound(typ))
}

// freeName is the name of the free helper of the type typ stands for.
func (g *generator) freeName(typ types.Type) string {
	return freeNameOf(g.prog.Types.Bound(typ))
}

// equalityName is the name of the equality helper of the type typ stands
// for.
func (g *generator) equalityName(typ types.Type) string {
	return equalityNameOf(g.prog.Types.Bound(typ))
}

// funcPtrName is the C pointer type of the function type typ stands for.
func (g *generator) funcPtrName(typ types.Type) string {
	return funcPtrNameOf(g.prog.Types.Bound(typ))
}

// funcResultsName is the C struct a function of the function type typ
// stands for, with several results, returns them in through a value.
func (g *generator) funcResultsName(typ types.Type) string {
	return funcResultsNameOf(g.prog.Types.Bound(typ))
}
