package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Switches on a type parameter. switch T { case int64: ... } is checked
// once, whatever T stands for: T must be a type parameter constrained by
// Data, and each case value a type T may stand for (see typeCase): a
// type that is Data and no struct but text.Text, int64 or text.Text;
// list[E] or map[text.Text]V, whose E or V is a type parameter of the
// case's own, constrained by Data; or struct, any struct. Data is
// closed, so the types T may stand for fall into a fixed set of kinds
// (see dataKind): each integer and float type, bool, text.Text, lists,
// maps and every other struct. Cases match first to last, so a value
// whose kinds every value before it took never matches, and is
// reported, as is a value written twice; a switch with no default must
// take every kind T may be, and one with a default that never runs, all
// of them taken, is reported, as a switch on a union's is. A switch
// nested in a case that leaves T fewer kinds, struct, a default or a
// case naming several types, is held to those (see Checker.paramKinds).
//
// A case naming one type narrows T in its body: every value of type T,
// a parameter, a local or a mut place declared outside the case, is of
// that type there, list[E] for list[E], and every type made of T is made
// of it, so v + 1, a == b, v.bytes() and a fact about v hold as they
// would for a value declared of that type (see narrowType and
// Ident.Narrowed). A case naming several types, struct and the default
// narrow nothing. Since nothing in a case is checked against any other
// type T may stand for, and only the case its type argument matches is
// lowered in each instance, nothing in the other cases is ever wrong for
// it. A switch with no default that takes every kind is marked
// exhaustive, so one whose every case returns ends a body as a return
// does. A fallthrough is reported, as in a switch on a union.
//
// A variable declared outside the switch and named in a case is marked
// Unrolled, as one named in a fields loop is: no case but one is lowered
// for an instance, so C may never see it named.

// dataKind is one of the kinds of type a type parameter constrained by
// Data may stand for, each a bit of a kindSet.
type dataKind int

const (
	kindBool dataKind = iota
	kindInt8
	kindInt16
	kindInt32
	kindInt64
	kindUint8
	kindUint16
	kindUint32
	kindUint64
	kindSize
	kindUintptr
	kindFloat32
	kindFloat64
	kindText
	kindList
	kindMap
	kindStruct
	numKinds
)

// kindNames holds each kind's name as a case would name it.
var kindNames = [numKinds]string{
	kindBool: "bool", kindInt8: "int8", kindInt16: "int16", kindInt32: "int32",
	kindInt64: "int64", kindUint8: "uint8", kindUint16: "uint16", kindUint32: "uint32",
	kindUint64: "uint64", kindSize: "unsafe_size_t", kindUintptr: "unsafe_uintptr_t",
	kindFloat32: "float32", kindFloat64: "float64", kindText: "text.Text",
	kindList: "list[E]", kindMap: "map[text.Text]V", kindStruct: "struct",
}

// basicKinds maps each basic type that is Data to its kind.
var basicKinds = map[types.Type]dataKind{
	types.Bool: kindBool, types.Int8: kindInt8, types.Int16: kindInt16,
	types.Int32: kindInt32, types.Int64: kindInt64, types.Uint8: kindUint8,
	types.Uint16: kindUint16, types.Uint32: kindUint32, types.Uint64: kindUint64,
	types.UnsafeSize: kindSize, types.UnsafeUintptr: kindUintptr,
	types.Float32: kindFloat32, types.Float64: kindFloat64,
}

// kindSet is a set of dataKinds.
type kindSet uint32

// allKinds holds every kind.
const allKinds kindSet = 1<<numKinds - 1

// kindBit returns the set holding k alone.
func kindBit(k dataKind) kindSet { return 1 << k }

// names returns the names of the kinds in s, in order.
func (s kindSet) names() []string {
	var out []string
	for k := range numKinds {
		if s&kindBit(k) != 0 {
			out = append(out, kindNames[k])
		}
	}
	return out
}

// narrowing is a case of a switch on a type parameter in force around
// the code being checked: each type parameter it narrows, mapped to the
// type it is there, each made of no type parameter another maps.
type narrowing map[types.Type]types.Type

// narrowType returns typ as it is where checking is: each type parameter
// a case around it narrows replaced, in typ and in every type typ is made
// of, by the type that case narrows it to, and typ itself outside every
// such case.
func (c *Checker) narrowType(typ types.Type) types.Type {
	if len(c.narrowed) == 0 || typ == types.Invalid {
		return typ
	}
	return c.substitute(typ, c.narrowed)
}

// narrowIdent records on id, a name of a variable or a parameter whose
// declared type is typ, the type its value is where id is written, when
// a case around it narrows that type, and returns that type.
func (c *Checker) narrowIdent(id *ast.Ident, typ types.Type) types.Type {
	id.Narrowed = types.Invalid
	if to := c.narrowType(typ); to != typ {
		id.Narrowed = to
		return to
	}
	return typ
}

// narrow runs check with param narrowed to to, a type made of no type
// parameter narrowed already but param, in every type narrowed so far.
func (c *Checker) narrow(param, to types.Type, check func()) {
	outer := c.narrowed
	inner := make(narrowing, len(outer)+1)
	for k, v := range outer {
		inner[k] = c.substitute(v, narrowing{param: to})
	}
	inner[param] = to
	c.narrowed = inner
	check()
	c.narrowed = outer
}

// withKinds runs check with param held to kinds, the kinds a case it is
// not narrowed in leaves it.
func (c *Checker) withKinds(param types.Type, kinds kindSet, check func()) {
	outer, had := c.paramKinds[param]
	c.paramKinds[param] = kinds
	check()
	if had {
		c.paramKinds[param] = outer
	} else {
		delete(c.paramKinds, param)
	}
}

// kindsOf returns the kinds the type parameter param may stand for where
// checking is: those the cases around it leave it, every kind outside
// any.
func (c *Checker) kindsOf(param types.Type) kindSet {
	if kinds, ok := c.paramKinds[param]; ok {
		return kinds
	}
	return allKinds
}

// checkParamSwitch checks s, a switch in fn on a type parameter (see the
// comment at the top of the file): its tag, each case's values in source
// order with what each takes, its body, narrowed when it names one type,
// and the coverage of the whole. A switch whose tag was reported checks
// its cases' values and bodies all the same, with nothing narrowed, and
// neither it nor one with a value reported has its coverage reported: a
// value reported may have named what is missing. Without a default it is
// taken as covering everything, so nothing after it is reported for it.
func (c *Checker) checkParamSwitch(s *ast.Switch, fn *ast.Func) {
	param, ok := c.switchParam(s)
	possible := allKinds
	if ok {
		possible = c.kindsOf(param)
	}
	taken := kindSet(0)
	takenBy := make(map[dataKind]ast.Expr)
	seen := make(map[string]ast.Expr)
	reported := !ok || hasBadClause(s)
	for _, cl := range s.Cases {
		if cl.Default {
			continue
		}
		arm, armOK := c.caseTypes(cl, c.typeName(param), possible, taken, takenBy, seen)
		reported = reported || !armOK
		for k := range numKinds {
			if arm&kindBit(k) != 0 && takenBy[k] == nil {
				takenBy[k] = c.caseValueFor(cl, k)
			}
		}
		taken |= arm
		c.checkParamCase(cl, param, ok, arm, fn)
	}
	left := possible &^ taken
	if d := s.Default(); d != nil {
		never := ok && left == 0 && !reported
		if never {
			c.errorf(
				d.Span,
				"every type %s can stand for is already covered, so this default never runs",
				c.typeName(param),
			)
			c.inCoveredDefault++
		}
		c.checkParamCase(d, param, ok, left, fn)
		if never {
			c.inCoveredDefault--
		}
		return
	}
	switch {
	case reported:
		c.markExhaustive(s)
	case left != 0:
		missing := left.names()
		pronoun := "it"
		if len(missing) > 1 {
			pronoun = "them"
		}
		header := ast.Span{File: s.Span.File, Start: s.Span.Start, End: s.Tag.Spanned().End}
		c.errorf(header, "switch on %s misses %s %s; cover %s or add a default",
			c.typeName(param), plural(len(missing), "case"), strings.Join(missing, ", "), pronoun)
	default:
		c.markExhaustive(s)
	}
}

// caseValueFor returns the value of cl, a case of a switch on a type
// parameter, that takes kind k, for a note to point at.
func (c *Checker) caseValueFor(cl *ast.CaseClause, k dataKind) ast.Expr {
	for i, tc := range cl.TypeCases {
		if c.caseKinds(tc)&kindBit(k) != 0 && i < len(cl.Values) {
			return cl.Values[i]
		}
	}
	return nil
}

// switchParam returns the type parameter s, a switch on a type parameter,
// switches on, and whether it is one a switch can be on: constrained by
// Data, and not narrowed by a case around s. Anything else is reported.
func (c *Checker) switchParam(s *ast.Switch) (types.Type, bool) {
	id, _ := s.Tag.(*ast.Ident)
	if id == nil || c.unbound(id) {
		return types.Invalid, false
	}
	tp, isParam := id.Def.(*ast.TypeParam)
	if !isParam || tp.Type == types.Invalid {
		if !isParam {
			c.errorf(id.Span, "cannot switch on %s: it is not a type parameter", id.Name)
		}
		return types.Invalid, false
	}
	if to, narrowed := c.narrowed[tp.Type]; narrowed {
		c.errorf(id.Span, "cannot switch on %s: it is %s in this case", id.Name, c.typeName(to))
		return types.Invalid, false
	}
	switch {
	case tp.Data:
		return tp.Type, true
	case tp.Iface != nil:
		c.errorf(id.Span, "cannot switch on %s: it is constrained by %s, not Data",
			id.Name, c.ifaceName(tp.Iface))
	}
	return types.Invalid, false
}

// caseTypes checks the values of cl, a case of a switch on the type
// parameter name, which may stand for the kinds possible, records how each
// matches in cl.TypeCases, and returns the kinds the case takes: those its
// values match that no case before it took, which taken holds, each
// recorded in takenBy with the value that took it. A value that names the
// type or the struct an earlier one named, which seen records, is
// reported, and so is
// one that never matches, every kind it would take taken already or not
// possible here, and a list[E] or map[text.Text]V among several values,
// whose type parameter would stand for nothing in particular. It reports
// whether no value was reported, nor left reported by an earlier stage.
func (c *Checker) caseTypes(
	cl *ast.CaseClause,
	name string,
	possible, taken kindSet,
	takenBy map[dataKind]ast.Expr,
	seen map[string]ast.Expr,
) (kindSet, bool) {
	cl.TypeCases = cl.TypeCases[:0]
	ok := true
	arm := kindSet(0)
	for _, v := range cl.Values {
		tc, key, valid := c.typeCase(v)
		if !valid {
			ok = false
			continue
		}
		cl.TypeCases = append(cl.TypeCases, tc)
		if prev, dup := seen[key]; dup {
			c.diags = append(c.diags, diag.Diagnostic{
				Severity: diag.Error,
				Message:  fmt.Sprintf("%s appears twice in this switch", key),
				Span:     v.Spanned(),
				Related: []diag.Diagnostic{
					{Severity: diag.Note, Message: "previous case", Span: prev.Spanned()},
				},
			})
			continue
		}
		if key != "" {
			seen[key] = v
		}
		if (tc.Kind == ast.TypeList || tc.Kind == ast.TypeMap) && len(cl.Values) > 1 {
			part := "value type of the map"
			if tc.Kind == ast.TypeList {
				part = "element type of the list"
			}
			c.errorf(v.Spanned(), "a case naming %s names no other type, since %s stands for "+
				"the %s it matches", c.text(v.Spanned()), c.text(ast.CaseTypeName(v).Span), part)
			ok = false
			continue
		}
		kinds := c.caseKinds(tc)
		if fresh := kinds & possible &^ (taken | arm); fresh == 0 {
			c.neverMatches(v, name, kinds, possible, takenBy)
			continue
		}
		arm |= kinds & possible &^ taken
	}
	return arm, ok
}

// neverMatches reports v, a value of a case of a switch on the type
// parameter param, which takes kinds, none of them possible here or left by
// the values before it, which takenBy records, with a note at a value
// that took one, if any did.
func (c *Checker) neverMatches(
	v ast.Expr,
	param string,
	kinds, possible kindSet,
	takenBy map[dataKind]ast.Expr,
) {
	d := diag.Diagnostic{Severity: diag.Error, Span: v.Spanned()}
	name := c.text(v.Spanned())
	if kinds&possible == 0 {
		d.Message = fmt.Sprintf("case %s never matches: %s is never %s here", name, param, name)
		c.diags = append(c.diags, d)
		return
	}
	d.Message = fmt.Sprintf(
		"case %s never matches: an earlier case takes every type it names",
		name,
	)
	for k := range numKinds {
		if kinds&possible&kindBit(k) != 0 && takenBy[k] != nil {
			d.Related = []diag.Diagnostic{{
				Severity: diag.Note, Message: "earlier case", Span: takenBy[k].Spanned(),
			}}
			break
		}
	}
	c.diags = append(c.diags, d)
}

// caseKinds returns the kinds tc matches.
func (c *Checker) caseKinds(tc ast.TypeCase) kindSet {
	switch tc.Kind {
	case ast.TypeList:
		return kindBit(kindList)
	case ast.TypeMap:
		return kindBit(kindMap)
	case ast.TypeStruct:
		return kindBit(kindStruct) | kindBit(kindText)
	}
	if k, ok := basicKinds[tc.Type]; ok {
		return kindBit(k)
	}
	return kindBit(kindText)
}

// typeCase checks v, a value of a case of a switch on a type parameter,
// and returns how it matches, a key naming what it names, for a value
// naming the same type or struct again, "" for a list[E] or a
// map[text.Text]V, each of which names a type parameter of its own, and
// whether it is one a case can name; one that is not is reported, unless
// an earlier stage reported it.
func (c *Checker) typeCase(v ast.Expr) (ast.TypeCase, string, bool) {
	switch x := v.(type) {
	case *ast.StructCase:
		return ast.TypeCase{Kind: ast.TypeStruct}, "struct", true
	case *ast.ListType:
		tp := c.caseParam(x.Elem)
		if tp == types.Invalid {
			return ast.TypeCase{}, "", false
		}
		return ast.TypeCase{Kind: ast.TypeList, Type: tp}, "", true
	case *ast.MapType:
		key := c.declaredType(x.Key)
		tp := c.caseParam(x.Value)
		if key == types.Invalid || tp == types.Invalid {
			return ast.TypeCase{}, "", false
		}
		if !c.isText(key) {
			c.errorf(
				x.Key.Spanned(),
				"case %s never matches: a Data map's key is text.Text, not %s",
				c.text(v.Spanned()),
				c.typeName(key),
			)
			return ast.TypeCase{}, "", false
		}
		return ast.TypeCase{Kind: ast.TypeMap, Type: tp, Key: key}, "", true
	}
	typ := c.declaredType(v)
	name := c.text(v.Spanned())
	switch {
	case typ == types.Invalid:
		return ast.TypeCase{}, "", false
	case c.table.IsTypeParam(typ):
		c.errorf(v.Spanned(), "case %s names a type parameter; a case of a switch on a type "+
			"parameter names a type, list[E], map[text.Text]V or struct", name)
		return ast.TypeCase{}, "", false
	}
	if why := c.notData(typ, make(map[types.Type]bool)); why != "" {
		c.errorf(v.Spanned(), "case %s never matches: %s does not satisfy Data: %s",
			name, c.typeName(typ), why)
		return ast.TypeCase{}, "", false
	}
	if c.table.IsStruct(typ) && !c.isText(typ) {
		c.errorf(v.Spanned(), "case %s names a struct type; a case of a switch on a type "+
			"parameter names text.Text or struct, which matches every struct", name)
		return ast.TypeCase{}, "", false
	}
	return ast.TypeCase{Kind: ast.TypeExact, Type: typ}, c.typeName(typ), true
}

// caseParam returns the type parameter part, the name a case's list[E]
// or map[text.Text]V writes for its element or value type, declares,
// giving it a type of its own, constrained by Data, the first time, or
// types.Invalid when the resolver declared none, having reported why.
func (c *Checker) caseParam(part ast.Expr) types.Type {
	id, ok := part.(*ast.Ident)
	if !ok {
		return types.Invalid
	}
	tp, ok := id.Def.(*ast.TypeParam)
	if !ok {
		return types.Invalid
	}
	if tp.Type == types.Invalid {
		tp.Type = c.table.TypeParam(tp.Name)
		tp.Data = true
		c.typeParams[tp.Type] = tp
	}
	return tp.Type
}

// checkParamCase checks the body of cl, a case of a switch on the type
// parameter param, which ok says is one, that takes the kinds arm:
// with param narrowed to the one type it names, if it names one and takes
// a kind, and held to arm otherwise. A case whose one value was reported
// narrows param to types.Invalid, so nothing its body does with a value
// of it is reported again. A variable declared outside the switch and
// named in it is marked Unrolled (see markUnrolled).
func (c *Checker) checkParamCase(
	cl *ast.CaseClause,
	param types.Type,
	ok bool,
	arm kindSet,
	fn *ast.Func,
) {
	check := func() {
		c.inClause(cl, paramSwitch, func() {
			c.checkGuarded(cl.Body, &guard{span: caseSpan(cl), kind: guardCase}, nil, fn)
		})
	}
	defer markUnrolled(cl.Body)
	switch {
	case !ok:
		check()
	case !cl.Default && len(cl.Values) == 1 && len(cl.TypeCases) == 0:
		c.narrow(param, types.Invalid, check)
	case !cl.Default && len(cl.TypeCases) == 1 && len(cl.Values) == 1 && arm != 0 &&
		cl.TypeCases[0].Kind != ast.TypeStruct:
		c.narrow(param, c.narrowedTo(cl.TypeCases[0]), check)
	default:
		c.withKinds(param, arm, check)
	}
}

// narrowedTo returns the type a case whose one value matches as tc
// narrows its type parameter to: the type it names, or a list or a map
// of the type parameter it declares.
func (c *Checker) narrowedTo(tc ast.TypeCase) types.Type {
	switch tc.Kind {
	case ast.TypeList:
		return c.table.List(tc.Type)
	case ast.TypeMap:
		return c.table.Map(tc.Key, tc.Type)
	default:
		return tc.Type
	}
}
