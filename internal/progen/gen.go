package progen

import (
	"fmt"
	"strings"
)

// Var is a variable or a parameter of a generated program.
type Var struct {
	T     *Type
	Name  string
	Loops int  // how many loops enclose its declaration
	Mut   bool // declared mut: it may be written, or passed as mut
	Used  bool // read somewhere, so not reported as declared and not used
	Moved bool // passed as own, so it may no longer be used
	Param bool // a parameter, which borrows its caller's value
	Own   bool // an own parameter, which owns its value and may be moved on
}

// Expr is a generated expression: its Tack text, its type, and how the
// interpreter computes it.
type Expr struct {
	Eval  func(*frame) Value
	T     *Type
	Text  string
	Konst bool // a constant expression, made only of literals
}

// Stmt is a generated statement: its Tack lines, each indented relative
// to the statement, and how the interpreter runs it.
type Stmt struct {
	Exec  func(*frame) ctrl
	Lines []string
}

// Func is a generated function.
type Func struct {
	Result    *Type
	Recv      *Var // a method's receiver, nil for a function
	Name      string
	Params    []*Var
	Body      []Stmt
	Text      []string
	Cost      int  // how many steps a call may take, at most, as estimated
	Index     int  // its position among the functions generated
	ErrResult bool // it returns an error after its result
}

// scope is one block's variables.
type scope struct {
	vars []*Var
}

// Gen generates one program from a Source. Its zero value is not usable;
// Generate makes one.
type Gen struct {
	fn       *Func
	frozen   map[*Var]bool  // variables that must not be written here
	stats    map[string]int // how many of each statement kind were made
	hidden   map[*Var]bool  // variables no expression may name here
	imports  map[string]bool
	extras   map[string]string // helper declarations a program needs, by name
	order    []string          // the names of extras, in the order they were needed
	src      Source
	structs  []*StructDecl
	unions   []*UnionDecl
	funcs    []*Func // the functions generated so far, which later ones may call
	scopes   []*scope
	names    int
	depth    int  // how deeply blocks nest where generation is
	loops    int  // how many loops enclose where generation is
	mult     int  // how many times, at most, the code being generated runs per call
	cost     int  // the estimated cost of the function being generated
	noLists  bool // no expression may name a list or a map, which is passed as mut
	inSwitch bool // inside a switch's clause, where break and continue are not made
	inDefer  bool // inside deferred code, which may not break, continue or return
}

// Limits on a generated program's size, which keep it small and quick.
const (
	maxFuncs     = 5
	maxStmts     = 6
	maxTopStmts  = 14 // a function body's own statements, outside any nested block
	maxDepth     = 3
	maxExprDepth = 3
	maxTrips     = 4
	costBudget   = 3000
	assumedLen   = 16 // how many elements a range over a list or a map is costed at
)

// pick returns a choice in [0, n).
func (g *Gen) pick(n int) int {
	return g.src.Intn(n)
}

// rare reports whether to take a choice made one time in n, never when
// the Source has run out.
func (g *Gen) rare(n int) bool {
	return g.pick(n) == n-1
}

// more reports whether to go on adding to a list of n items so far, at
// most limit of them; a Source run out stops at once.
func (g *Gen) more(n, limit int) bool {
	return n < limit && g.pick(8) != 0
}

// name returns a fresh name starting with prefix.
func (g *Gen) name(prefix string) string {
	g.names++
	return fmt.Sprintf("%s%d", prefix, g.names)
}

// push opens a block's scope.
func (g *Gen) push() {
	g.scopes = append(g.scopes, &scope{})
	g.depth++
}

// pop closes the innermost scope, returning statements that read each of
// its variables never read, which Tack would report as unused.
func (g *Gen) pop() []Stmt {
	sc := g.scopes[len(g.scopes)-1]
	g.scopes = g.scopes[:len(g.scopes)-1]
	g.depth--
	var reads []Stmt
	for _, v := range sc.vars {
		if !v.Used {
			reads = append(reads, g.observeVar(v))
		}
	}
	return reads
}

// declare adds v to the innermost scope.
func (g *Gen) declare(v *Var) {
	v.Loops = g.loops
	sc := g.scopes[len(g.scopes)-1]
	sc.vars = append(sc.vars, v)
}

// visible returns every variable in scope for which keep holds, but those
// moved and those hidden here.
func (g *Gen) visible(keep func(*Var) bool) []*Var {
	var out []*Var
	for _, sc := range g.scopes {
		for _, v := range sc.vars {
			if !v.Moved && !g.hidden[v] && keep(v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// choose returns one of vs, or nil when there are none.
func (g *Gen) choose(vs []*Var) *Var {
	if len(vs) == 0 {
		return nil
	}
	return vs[g.pick(len(vs))]
}

// writable reports whether v may be written where generation is.
func (g *Gen) writable(v *Var) bool {
	return v.Mut && !g.frozen[v]
}

// block generates a block's statements in a new scope, up to maxStmts,
// none past maxDepth.
func (g *Gen) block() []Stmt {
	g.push()
	var stmts []Stmt
	for g.depth <= maxDepth && g.more(len(stmts), maxStmts) {
		stmts = append(stmts, g.stmt())
	}
	return append(stmts, g.pop()...)
}

// indent returns lines indented one level, under a header and above a
// closing brace.
func indent(head string, body []Stmt) []string {
	lines := []string{head}
	for _, s := range body {
		for _, l := range s.Lines {
			lines = append(lines, "    "+l)
		}
	}
	return append(lines, "}")
}

// joinArgs joins the texts of args with commas.
func joinArgs(args []Expr) string {
	ts := make([]string, len(args))
	for i, a := range args {
		ts[i] = a.Text
	}
	return strings.Join(ts, ", ")
}
