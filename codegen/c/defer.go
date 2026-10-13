package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A defer emits nothing where it is written. Each block that reaches
// defers ends in its cleanup chain: its defers, last written first, each
// lowered exactly once. Falling off the block's end runs the whole chain.
// A return, break or continue that leaves the block jumps into the chain
// with a C goto, at the label before the last defer it reached, so it runs
// that defer and every earlier one, and none it did not reach:
//
//	tack_defer0_2: ;
//	second defer
//	tack_defer0_1: ;
//	first defer
//	if (tack_next1 == 3) goto tack_defer2_1;
//
// Only labels some exit jumps to are written, since clang warns about an
// unused label. A block no exit leaves ends in its defers alone.
//
// No flag records whether a defer was reached, since none is ever needed:
// Tack has no goto and no labels, so a statement of a block runs only after
// every statement before it in that block has finished, and a point that
// leaves the block from inside a later statement has, every time it runs,
// passed every defer written before that statement in the block. Which
// label each exit jumps to is therefore known from where it is written.
//
// After a chain, an exit that leaves further blocks with defers to run
// goes on to the next one's chain, and one that leaves none ends: break
// and continue jump, return returns. An int, the frame's next variable,
// says which way: the exit stores a number naming its route, and each
// chain it passes ends in a test of that number. Exits that take the same
// route share a number. An exit whose route is a single chain it falls
// from, as below, stores none. A block whose chain has tests, and whose end
// can be reached by falling through, clears the variable before its chain,
// and a break clears it as it leaves its loop, so a number a finished exit
// left behind never misroutes a later fall through, or a later continue
// that jumps into its loop body's chain without storing a number, where
// only breaks and returns are tested.
//
// Each deferred body is its own frame, with its own next variable, since a
// loop in it may break or continue through its own defers while the exit
// that is running it is still on its way; no exit leaves a deferred body.
// A function body is the outermost frame.
//
// A return evaluates its values first, into the function's ret variable,
// declared at the top of the function with its result type and zero value,
// then jumps, so a defer that changes a variable used in the returned
// expression does not change what is returned. A return whose route ends
// at the function body's chain falls from it to the function's end, which
// returns ret; its route needs no test there. The same final return keeps
// clang from warning that a function with a result may end without one.
// A continue whose route ends at the loop body's chain falls through to
// the body's end, as an iteration does, and so runs the defers before a
// three-clause loop's post statement.
//
// Each defer is lowered once, so the C is linear in the source: an exit
// adds a store, a goto and one test to each chain on its route. A goto only
// jumps forward, to the end of a block enclosing it, never into a block,
// and no variably modified type exists to be jumped into the scope of. It
// can jump past declarations later in the block, which C allows, leaving
// them uninitialized; deferred code never reads them, since a defer can
// only name variables declared before it, and every exit into the chain at
// a defer has passed it, and so them.

// deferScope is what the generator knows about one block while lowering
// it: the defers reached so far in it, in order, whether it is a loop's
// body, which a break or continue leaves, and what its cleanup chain needs.
type deferScope struct {
	entries map[int]bool // the labels jumped to, by how many defers they run
	routed  map[int]bool // the route numbers it already tests
	label   string       // the prefix of its labels, named when first needed
	defers  []*ast.Defer
	routes  []string // the tests at the end of its chain
	loop    bool
}

// deferFrame is what the generator knows about the frame being lowered: a
// function body or a deferred body.
type deferFrame struct {
	routes map[string]int // route numbers, by route
	next   string         // the next variable, named when first needed
	ret    string         // the function's ret variable, likewise
}

// stop is one block on an exit's route: one with defers to run, and how
// many of them the exit has reached.
type stop struct {
	sc    *deferScope
	entry int
}

// enter returns the label before sc's defer number entry, counting from
// 1, which runs it and every defer before it, and records it as used.
// Labels are numbered apart from temporaries, since C keeps them in a
// namespace of their own.
func (g *generator) enter(sc *deferScope, entry int) string {
	if sc.label == "" {
		sc.label = fmt.Sprintf("tack_defer%d", g.labels)
		g.labels++
		sc.entries = make(map[int]bool)
		sc.routed = make(map[int]bool)
	}
	sc.entries[entry] = true
	return fmt.Sprintf("%s_%d", sc.label, entry)
}

// route returns the number of the route through the chains labels names,
// one label per stop, that ends in final, numbering it if it is new. The
// first stop is named by its block's label prefix, since the exit jumps
// into it directly, so exits from different points of one block that go
// on the same way share a number.
func (f *deferFrame) route(first *deferScope, labels []string, final string) int {
	key := first.label + " " + strings.Join(labels[1:], " ") + " " + final
	if f.routes == nil {
		f.routes = make(map[string]int)
	}
	if id, ok := f.routes[key]; ok {
		return id
	}
	id := len(f.routes) + 1
	f.routes[key] = id
	return id
}

// stops returns the route of an exit that leaves the innermost n blocks:
// those of them with defers reached, innermost first.
func (g *generator) stops(n int) []stop {
	var route []stop
	for i := len(g.scopes) - 1; i >= len(g.scopes)-n; i-- {
		if k := len(g.scopes[i].defers); k > 0 {
			route = append(route, stop{g.scopes[i], k})
		}
	}
	return route
}

// leave lowers an exit along route, which is not empty, final being the
// C statement it ends with after the last chain, or "" when it falls from
// that chain to where it is going.
func (g *generator) leave(route []stop, final string) []string {
	labels := make([]string, len(route))
	for i, st := range route {
		labels[i] = g.enter(st.sc, st.entry)
	}
	if len(route) == 1 && final == "" {
		return []string{fmt.Sprintf("goto %s;", labels[0])}
	}
	next := g.nextVar()
	id := g.frame.route(route[0].sc, labels, final)
	for i, st := range route {
		action := final
		if i+1 < len(route) {
			action = fmt.Sprintf("goto %s;", labels[i+1])
		}
		if action != "" && !st.sc.routed[id] {
			st.sc.routed[id] = true
			st.sc.routes = append(st.sc.routes, fmt.Sprintf("if (%s == %d) %s", next, id, action))
		}
	}
	return []string{fmt.Sprintf("%s = %d;", next, id), fmt.Sprintf("goto %s;", labels[0])}
}

// nextVar returns the name of the frame's next variable, naming it if it
// is the first use.
func (g *generator) nextVar() string {
	if g.frame.next == "" {
		g.frame.next = g.temp("next")
	}
	return g.frame.next
}

// deferredReturn lowers a return that leaves blocks with defers to run,
// along route. value is its rendered value, or "" for a bare return.
func (g *generator) deferredReturn(value string, route []stop) []string {
	var lines []string
	final := "return;"
	if value != "" {
		if g.frame.ret == "" {
			g.frame.ret = g.temp("ret")
		}
		lines = append(lines, fmt.Sprintf("%s = %s;", g.frame.ret, value))
		final = fmt.Sprintf("return %s;", g.frame.ret)
	}
	if route[len(route)-1].sc == g.scopes[0] {
		final = ""
	}
	return append(lines, g.leave(route, final)...)
}

// generateJump lowers a break or continue, jump being its C statement. One
// that leaves blocks with defers to run, up to and including the body of
// the innermost loop, takes the route through their chains.
func (g *generator) generateJump(jump string) []string {
	body := g.scopes[0]
	n := len(g.scopes)
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if g.scopes[i].loop {
			body, n = g.scopes[i], len(g.scopes)-i
			break
		}
	}
	route := g.stops(n)
	if len(route) == 0 {
		return []string{jump}
	}
	switch {
	case jump == "break;":
		jump = fmt.Sprintf("{ %s = 0; break; }", g.nextVar())
	case route[len(route)-1].sc == body:
		jump = ""
	}
	return g.leave(route, jump)
}

// chain lowers the end of sc's block, body: the clearing of the next
// variable when its chain has tests and its end can be fallen into, then
// its defers, last first, each after its label if one is jumped to, then
// its tests.
func (g *generator) chain(
	sc *deferScope,
	body []ast.Stmt,
	fn *ast.Func,
) ([]string, []diag.Diagnostic) {
	var lines []string
	if len(sc.routes) > 0 && !endsInJump(body) {
		lines = append(lines, g.frame.next+" = 0;")
	}
	for j := len(sc.defers); j >= 1; j-- {
		if sc.entries[j] {
			lines = append(lines, fmt.Sprintf("%s_%d: ;", sc.label, j))
		}
		got, diags := g.deferred(sc.defers[j-1], fn)
		if len(diags) > 0 {
			return nil, diags
		}
		lines = append(lines, got...)
	}
	return append(lines, sc.routes...), nil
}

// deferred lowers what d defers, once. It is generated as a frame of its
// own, with no enclosing blocks, since nothing in it leaves it, so its own
// defers run at its end and a break or continue in it finds the loop it is
// in. A deferred block becomes a C block, which scopes the variables it
// declares, its frame's next variable among them.
func (g *generator) deferred(d *ast.Defer, fn *ast.Func) ([]string, []diag.Diagnostic) {
	outer, outerFrame := g.scopes, g.frame
	g.scopes, g.frame = nil, &deferFrame{}
	body, diags := g.generateBlock(d.Body, fn)
	frame := g.frame
	g.scopes, g.frame = outer, outerFrame
	if len(diags) > 0 {
		return nil, diags
	}
	if frame.next != "" {
		body = append([]string{fmt.Sprintf("int %s = 0;", frame.next)}, body...)
	}
	if !d.Block && frame.next == "" {
		return body, nil
	}
	return append(append([]string{"{"}, indent(body)...), "}"), nil
}

// funcFrame lowers fn's body as the outermost frame: its variables are
// declared first, and when a return holds its value in ret, the body ends
// by returning it.
func (g *generator) funcFrame(fn *ast.Func) ([]string, []diag.Diagnostic) {
	outer := g.frame
	g.frame = &deferFrame{}
	defer func() { g.frame = outer }()

	body, diags := g.generateBlock(fn.Body, fn)
	if len(diags) > 0 {
		return nil, diags
	}
	var decls []string
	if g.frame.ret != "" {
		decls = append(
			decls,
			fmt.Sprintf("%s %s = %s;", g.resultType(fn), g.frame.ret, g.resultZero(fn)),
		)
		body = append(body, fmt.Sprintf("return %s;", g.frame.ret))
	}
	if g.frame.next != "" {
		decls = append(decls, fmt.Sprintf("int %s = 0;", g.frame.next))
	}
	return append(decls, body...), nil
}

// resultZero is a C zero value of fn's result type, for a function that
// declares at least one result, to start ret with. ret is always assigned
// before it is returned, so a string starts zeroed in full, a null pointer
// and a length of 0, which needs no pooled empty string.
func (g *generator) resultZero(fn *ast.Func) string {
	if len(fn.ResultTypes) > 1 {
		return fmt.Sprintf("(%s){0}", g.resultsName(fn.Name))
	}
	if typ := fn.ResultTypes[0]; typ == types.String {
		return fmt.Sprintf("(%s){0}", g.cType(typ))
	}
	return g.zeroValue(fn.ResultTypes[0])
}

// endsInJump reports whether a block's last statement is a return, break
// or continue, so its end cannot be reached by falling through.
func endsInJump(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	switch body[len(body)-1].(type) {
	case *ast.Return, *ast.Break, *ast.Continue:
		return true
	default:
		return false
	}
}
