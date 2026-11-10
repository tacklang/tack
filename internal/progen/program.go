package progen

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Program is a generated Tack program: its source, the C file of its two
// extern funcs, and the interpreter's run of it.
type Program struct {
	run    *Func
	Stats  map[string]int // how many of each statement kind it has
	Source string         // main.tck
	Funcs  int            // how many functions it has besides run
}

// CSource is the C file every generated program is built with: live,
// which reads c_allocator's count of live buffers, and setup, which makes
// c_allocator fail every allocation after the first n when the
// environment variable PG_FAIL_AFTER is n.
const CSource = `#include <stdint.h>
#include <stdlib.h>

#include <tack/tack.h>

int64_t pg_live(void) {
    return tack_c_allocator_live_();
}

void pg_setup(void) {
    const char *s = getenv("PG_FAIL_AFTER");
    if (s != NULL) {
        tack_c_allocator_fail_after_(strtoll(s, NULL, 10));
    }
}
`

// prelude is the start of every generated program: the extern funcs, the
// checksum h and the functions that fold values into it.
const prelude = `package main

import fmt from "std/fmt"

extern("pg_live") func live() int64

extern("pg_setup") func setup()

mut h int64 = 0

func obs(x int64) {
    h = h *% 31 +% x
}

func obsu(x uint64) {
    v, ok := int64(x)
    if ok {
        obs(v)
    } else {
        obs(7)
    }
}

func obsb(b bool) {
    if b {
        obs(1)
    } else {
        obs(2)
    }
}
`

// epilogue is the end of every generated program: main runs run, checks
// every buffer was freed and prints the checksum.
const epilogue = `
func main() {
    setup()
    run()
    if live() != 0 {
        print("leak\n")
        exit(3)
    }
    fmt.printf("%d\n", h)
}
`

// Generate returns the program src decides.
func Generate(src Source) *Program {
	g := &Gen{
		src: src, frozen: make(map[*Var]bool), stats: make(map[string]int),
		hidden: make(map[*Var]bool), imports: make(map[string]bool),
		extras: make(map[string]string),
	}
	var b strings.Builder
	b.WriteString(prelude)
	g.genTypes(&b)
	for n := 0; g.more(n, maxFuncs); n++ {
		fn := g.genFunc(g.name("f"), false)
		fn.Index = len(g.funcs)
		g.funcs = append(g.funcs, fn)
		b.WriteString("\n" + strings.Join(fn.Text, "\n") + "\n")
	}
	run := g.genFunc("run", true)
	b.WriteString("\n" + strings.Join(run.Text, "\n") + "\n")
	b.WriteString(epilogue)
	return &Program{
		run: run, Source: g.withImports(b.String()), Funcs: len(g.funcs), Stats: g.stats,
	}
}

// Expect runs the program in the interpreter, within budget steps, and
// returns what it prints, and false when it runs past the budget.
func (p *Program) Expect(budget int) (out string, ok bool) {
	m := &machine{budget: budget}
	defer func() {
		if r := recover(); r != nil {
			err, isErr := r.(error)
			if !isErr || !errors.Is(err, errBudget) {
				panic(r)
			}
			ok = false
		}
	}()
	call(m, p.run, nil, nil)
	return strconv.FormatInt(m.h, 10) + "\n", true
}

// Trace returns the program's source changed to print every value it
// observes, one per line, before the checksum, and the values the
// interpreter observes, in order, for finding where a run that printed
// the wrong checksum went wrong.
func (p *Program) Trace(budget int) (string, []int64) {
	m := &machine{budget: budget, traced: true}
	func() {
		defer func() { _ = recover() }()
		call(m, p.run, nil, nil)
	}()
	src := strings.Replace(p.Source, "    h = h *% 31 +% x\n",
		"    h = h *% 31 +% x\n    fmt.printf(\"%d\\n\", x)\n", 1)
	return src, m.trace
}

// genTypes declares the program's structs and its union, if any.
func (g *Gen) genTypes(b *strings.Builder) {
	for n := 0; g.more(n, 2); n++ {
		sd := &StructDecl{Name: g.name("S")}
		for i := 0; i == 0 || g.more(i, 3); i++ {
			t := boolType
			if g.pick(4) != 0 {
				t = intTypeOf(intTypes[g.pick(len(intTypes))])
			}
			sd.Fields = append(sd.Fields, Field{Name: string(rune('a' + i)), T: t})
		}
		g.structs = append(g.structs, sd)
		fmt.Fprintf(b, "\ntype %s struct {\n", sd.Name)
		for _, fd := range sd.Fields {
			fmt.Fprintf(b, "    %s %s\n", fd.Name, fd.T)
		}
		b.WriteString("}\n")
	}
	if g.rare(2) {
		g.genUnion(b)
	}
}

// genUnion declares a union of two or three cases, each with an integer
// payload or none.
func (g *Gen) genUnion(b *strings.Builder) {
	u := &UnionDecl{Name: g.name("U")}
	for i := 0; i < 2 || g.more(i, 3); i++ {
		c := UnionCase{Name: "u" + strconv.Itoa(i)}
		if g.pick(3) != 0 {
			c.Payload = intTypeOf(intTypes[g.pick(len(intTypes))])
		}
		u.Cases = append(u.Cases, c)
	}
	g.unions = append(g.unions, u)
	fmt.Fprintf(b, "\ntype %s union {\n", u.Name)
	for _, c := range u.Cases {
		if c.Payload == nil {
			fmt.Fprintf(b, "    %s\n", c.Name)
		} else {
			fmt.Fprintf(b, "    %s %s\n", c.Name, c.Payload)
		}
	}
	b.WriteString("}\n")
}

// genFunc generates a function named name: run, taking nothing and
// returning nothing, or another, with parameters and maybe a result, or
// a method of a struct.
func (g *Gen) genFunc(name string, isRun bool) *Func {
	fn := &Func{Name: name}
	g.fn, g.scopes, g.mult, g.cost, g.loops, g.depth = fn, nil, 1, 0, 0, 0
	g.push()
	if !isRun {
		g.genReceiver(fn)
		g.genSignature(fn)
	}
	body := g.blockIn()
	var final []Stmt
	if fn.Result != nil {
		final = []Stmt{g.returnStmt()}
	}
	body = append(append(body, g.pop()...), final...)
	fn.Body = body
	fn.Cost = g.cost + 1
	fn.Text = indent(signature(fn), body)
	return fn
}

// genSignature gives fn parameters, at most one a list, a map or a view,
// a list or a map passed plain, as mut or as own, and maybe a result, an
// integer or a bool, and maybe an error after it.
func (g *Gen) genSignature(fn *Func) {
	owned := false
	limit := 3
	if fn.Recv != nil && g.pick(2) == 0 {
		limit = 0 // a method of no parameters, which an interface can ask for
	}
	for n := 0; g.more(n, limit); n++ {
		p := &Var{Name: g.name("p"), T: g.paramType(&owned), Param: true}
		if p.T.owned() {
			switch g.pick(3) {
			case 1:
				p.Mut = true
			case 2:
				p.Own = true
			}
		}
		fn.Params = append(fn.Params, p)
		g.declare(p)
	}
	switch g.pick(4) {
	case 1, 2:
		fn.Result = intTypeOf(intTypes[g.pick(len(intTypes))])
	case 3:
		fn.Result = boolType
	}
	fn.ErrResult = fn.Result != nil && g.rare(4)
}

// genReceiver makes fn, now and then, a method of one of the program's
// structs, with a plain or a mut receiver.
func (g *Gen) genReceiver(fn *Func) {
	if len(g.structs) == 0 || !g.rare(3) {
		return
	}
	sd := g.structs[g.pick(len(g.structs))]
	fn.Recv = &Var{Name: g.name("r"), T: &Type{Kind: KStruct, Struct: sd}, Param: true}
	fn.Recv.Mut = g.pick(2) == 0
	fn.Name = g.name("m")
	g.declare(fn.Recv)
}

// paramType returns a parameter's type: a list, a map or a view only when
// owned is not yet set, which it then sets, a function type of a
// function generated so far, or a copyable type.
func (g *Gen) paramType(owned *bool) *Type {
	if !*owned && g.rare(4) {
		*owned = true
		switch g.pick(3) {
		case 0:
			return listOfInt64
		case 1:
			return mapOfInt64
		}
		return &Type{Kind: KView, Elem: int64Type}
	}
	if g.rare(5) {
		var opts []*Func
		for _, fn := range g.funcs {
			if valueFunc(fn) {
				opts = append(opts, fn)
			}
		}
		if len(opts) > 0 {
			return funcType(opts[g.pick(len(opts))])
		}
	}
	t := g.scalarType()
	if t.Kind == KString {
		return boolType
	}
	return t
}

// signature returns the first line of fn's declaration.
func signature(fn *Func) string {
	ps := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		ps[i] = paramText(p)
	}
	s := "func "
	if fn.Recv != nil {
		s += "(" + paramText(fn.Recv) + ") "
	}
	s += fn.Name + "(" + strings.Join(ps, ", ") + ")"
	switch {
	case fn.ErrResult:
		s += " (" + fn.Result.String() + ", error)"
	case fn.Result != nil:
		s += " " + fn.Result.String()
	}
	return s + " {"
}

// paramText returns p as a declaration writes it.
func paramText(p *Var) string {
	s := p.Name + " " + p.T.String()
	switch {
	case p.Mut:
		return "mut " + s
	case p.Own:
		return "own " + s
	}
	return s
}

// withImports returns src with an import of each std package the program
// uses, after its package clause and the import of std/fmt.
func (g *Gen) withImports(src string) string {
	var extra strings.Builder
	for _, pkg := range []string{"bytes", "conv", "errors", "sort", "text"} {
		if g.imports[pkg] {
			extra.WriteString("import " + pkg + " from \"std/" + pkg + "\"\n")
		}
	}
	for _, name := range g.order {
		src += "\n" + g.extras[name]
	}
	return strings.Replace(src, "import fmt from \"std/fmt\"\n",
		"import fmt from \"std/fmt\"\n"+extra.String(), 1)
}
