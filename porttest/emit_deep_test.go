package porttest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// emitLeaf is the statement each of emitNests ends in: a print the
// backend lowers, so that nothing between it and main's body is dead.
const emitLeaf = "print(\"x\")\n"

// emitMain returns a program of body in main, with prelude, declarations,
// before main.
func emitMain(prelude, body string) string {
	return "package main\n\n" + prelude + "func main() {\n" + body + "}\n"
}

// emitNests are the programs TestEmitDeep nests n levels deep, each one
// Go's --emit lowers: blocks of the statements that hold one, with a
// condition the backend cannot fold, expressions the backend recurses
// into a level at a time, regions, closures passed to a scope
// parameter, each three of the parser's levels, closures each writing a
// capture of the one around it, fields loops and switches on type
// parameters in an instance, calls through a function value, types
// nested in a parameter's, and in a generic function's type argument.
var emitNests = map[string]func(n int) string{
	"ifs": func(n int) string {
		return emitMain("", "b := len(\"a\") == 1\n"+rep("if b {\n", n)+emitLeaf+rep("}\n", n))
	},
	"elses": func(n int) string {
		return emitMain(
			"",
			"b := len(\"a\") == 1\n"+rep("if b {\n} else {\n", n)+emitLeaf+rep("}\n", n),
		)
	},
	"fors": func(n int) string {
		return emitMain("", rep("for len(\"a\") == 1 {\n", n)+emitLeaf+"break\n"+rep("}\n", n))
	},
	"ranges": func(n int) string { return emitMain("", rep("for range 3 {\n", n)+emitLeaf+rep("}\n", n)) },
	"switches": func(n int) string {
		return emitMain("", rep("switch {\ncase len(\"a\") == 1:\n", n)+emitLeaf+rep("}\n", n))
	},
	"regions": func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "region r%d {\n", i)
		}
		fmt.Fprintf(&b, "l := make(list[int64], r%d)\nif len(l) == 1 {\n%s}\n", n-1, emitLeaf)
		return emitMain("", b.String()+rep("}\n", n))
	},
	"closures": func(n int) string {
		return emitMain("func each(scope f func()) {\n    f()\n}\n\n",
			rep("each(func() {\n", n/3)+emitLeaf+rep("})\n", n/3))
	},
	"parens": func(n int) string {
		return emitMain(
			"",
			"x := "+rep("(", n)+"len(\"a\")"+rep(")", n)+"\nif x == 2 {\n"+emitLeaf+"}\n",
		)
	},
	"nots": func(n int) string {
		return emitMain(
			"",
			"b := "+rep("!(", n/2)+"len(\"a\") == 1"+rep(")", n/2)+"\nif b {\n"+emitLeaf+"}\n",
		)
	},
	"arraylits": func(n int) string {
		return emitMain("", "x := "+rep("[1]", n/2)+"int64"+rep("{", n/2)+"1"+rep("}", n/2)+
			"\nif x"+rep("[0]", n/2)+" == 2 {\n"+emitLeaf+"}\n")
	},
	"listtypes": func(n int) string { return emitMain("func use(own l "+listType(n)+") {\n}\n\n", "") },
	// Closures nested n/3 deep, each writing w, main's local, which
	// each reaches through the pointer the closure around it holds.
	"captures": func(n int) string {
		return emitMain("func each(scope f func()) {\n    f()\n}\n\n",
			"mut w := 0\n"+rep("each(func() {\nw +%= 1\n", n/3)+"if w > 0 {\n"+emitLeaf+"}\n"+rep("})\n", n/3))
	},
	// Fields loops nested in an instance, each unrolled once, a block a
	// level, each with its type parameter bound to the field's type.
	"fields": func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\ntype P struct {\n    v int64\n}\n\nfunc h[T Data](v T) {\n")
		for i := range n - 4 {
			fmt.Fprintf(&b, "for f%d := range fields(T) {\n", i)
		}
		b.WriteString(emitLeaf + rep("}\n", n-4) + "}\n\nfunc main() {\n    h(P{v: 1})\n}\n")
		return b.String()
	},
	// Switches on type parameters nested in an instance, each declaring
	// the element type of the list its type parameter stands for, which
	// the next switches on, two levels each.
	"param-switches": func(n int) string {
		m := (n - 8) / 2
		var b strings.Builder
		b.WriteString("package main\n\nfunc h[T Data](v T) {\nswitch T {\ncase list[E0]:\n")
		for i := range m - 1 {
			fmt.Fprintf(&b, "switch E%d {\ncase list[E%d]:\n", i, i+1)
		}
		b.WriteString(emitLeaf + rep("default:\n}\n", m))
		b.WriteString("}\n\nfunc main() {\n    l := make(" + listType(m) + ", c_allocator)\n    h(l)\n}\n")
		return b.String()
	},
	// Calls through a function value, each the argument of the next.
	"values": func(n int) string {
		return emitMain("func fi(x int64) int64 {\n    return x\n}\n\n",
			"f := fi\nx := "+rep("f(", n)+"len(\"a\")"+rep(")", n)+"\nif x == 2 {\n"+emitLeaf+"}\n")
	},
	// A generic function called with a list nested n-6 deep, whose
	// instance calls the instance for its element type, and so on down:
	// type arguments as deep as the parser lets a type nest, in a chain
	// of instances each instantiating the next.
	"generic-listtypes": func(n int) string {
		return "package main\n\nfunc gx[V Data](x V) int64 {\n    switch V {\n    case list[E]:\n" +
			"        mut n := 0\n        for _, e := range x {\n            n +%= gx(e)\n        }\n" +
			"        return n\n    default:\n        return 1\n    }\n}\n\nfunc use(own l " + listType(n-6) +
			") int64 {\n    return gx(l)\n}\n\nfunc main() {\n}\n"
	},
	"arraytypes": func(n int) string {
		return emitMain("func use(l "+rep("[1]", n)+"int64) {\n}\n\n", "")
	},
}

// emitChains are the programs TestEmitDeep writes n links long, which
// the backend must walk without a frame a link: an else if chain, a
// run of statements, an && chain, and locals each the next's value.
var emitChains = map[string]func(n int) string{
	"elseifs": func(n int) string {
		return emitMain(
			"",
			"x := len(\"a\")\nif x == 0 {\n}"+rep(" else if x == 1 {\n"+emitLeaf+"}", n)+"\n",
		)
	},
	"stmts": func(n int) string {
		return emitMain("", "b := len(\"a\") == 1\n"+rep("if b {\n"+emitLeaf+"}\n", n))
	},
	"ands": func(n int) string {
		return emitMain("", "b := "+rep("len(\"a\") == 1 && ", n)+"true\nif b {\n"+emitLeaf+"}\n")
	},
	"locals": func(n int) string {
		var b strings.Builder
		b.WriteString("a0 := len(\"a\")\n")
		for i := range n {
			fmt.Fprintf(&b, "a%d := a%d\n", i+1, i)
		}
		fmt.Fprintf(&b, "if a%d == 2 {\n%s}\n", n, emitLeaf)
		return emitMain("", b.String())
	},
}

// typeChainLength is how long TestEmitTypeChains's chains of structs
// are when Go runs them: Go's backend takes time quadratic in such a
// chain, some three and a half seconds at eight thousand links and ten
// at eight thousand owning ones, so a hundred thousand, chainLength,
// which TestEmitDeepPortOnly runs on the port alone, would take Go
// minutes.
const typeChainLength = 4000

// typeChains are the chains of structs, n links long, each holding the
// one before by value, that TestEmitTypeChains lowers: in a parameter,
// emitTypeDef's recursion; owning a list at the bottom, the free
// helpers'; compared, the equality helpers'; a map's key, the hashes';
// and an extern func's parameter, the header's.
var typeChains = map[string]func(n int) string{
	"plain": func(n int) string { return structChain(n, "v int64", "func use(x S%d) {\n}\n") },
	"owned": func(n int) string { return structChain(n, "l list[int64]", "func use(own x S%d) {\n}\n") },
	"compared": func(n int) string {
		return structChain(
			n,
			"v int64",
			"func eq(x S%[1]d, y S%[1]d) bool {\n    return x == y\n}\n",
		)
	},
	"keyed":  func(n int) string { return structChain(n, "v int64", "func k(own m map[S%d]int64) {\n}\n") },
	"header": func(n int) string { return structChain(n, "v int64", "extern(\"f\") func f(x S%d)\n") },
	// A fields loop's instance for each struct of the chain, each
	// instantiating the next's for its one field.
	"fields": func(n int) string {
		return structChain(n, "v int64", "func deep[T Data](v T) int64 {\n    mut n := 0\n"+
			"    for f := range fields(T) {\n        n = n +% 1 +% deep(v.[f])\n    }\n    return n\n}\n\n"+
			"func use(x S%d) int64 {\n    return deep(x)\n}\n")
	},
	// make of the outermost struct, whose empty value nests a compound
	// literal a link, down to the list the first holds.
	"made": func(n int) string {
		return structChain(n, "l list[int64]", "func use(scope alloc Allocator) S%[1]d {\n    return make(S%[1]d, alloc)\n}\n")
	},
}

// structChain returns a program of the structs S0, whose one field is
// bottom, to Sn, each holding the one before, use, a format of which
// every verb is n, using Sn, and an empty main.
func structChain(n int, bottom, use string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package main\n\ntype S0 struct {\n    %s\n}\n\n", bottom)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "type S%d struct {\n    s S%d\n}\n\n", i, i-1)
	}
	fmt.Fprintf(&b, use, n)
	b.WriteString("\nfunc main() {\n}\n")
	return b.String()
}

// emitTally records, for a deep test, each case it ran, compared or
// declined, with the emit stages it needs, so that the test can fail
// when it has cases whose every stage is required and they compared
// nothing (see tallyVerdict): a deep test of a stage must not pass by
// comparing nothing once its cases can be compared.
type emitTally struct {
	cases []tallyCase
	mu    sync.Mutex
}

// tallyCase is one case a deep test ran: the emit stages it needs, and
// whether it was compared, not declined.
type tallyCase struct {
	needs    emitSet
	compared bool
}

// note records in, a case of st, compared unless the port declined it,
// by the emit stages it needs, when any emit stage is required; while
// none is, check has nothing to check, and nothing is classified. It
// runs deferred, after a decline's skip too, so it reports a failure to
// classify with Error, not Fatal.
func (tl *emitTally) note(t *testing.T, st stage, in input, compared bool) {
	t.Helper()
	if !env.required[emitStageNames[emitE0]] {
		return
	}
	needs, err := classifyCase(st, in, false)
	if err != nil {
		t.Error(err)
		return
	}
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.cases = append(tl.cases, tallyCase{needs: needs, compared: compared})
}

// check fails t for each failure tallyVerdict finds in the cases noted.
func (tl *emitTally) check(t *testing.T) {
	t.Helper()
	tl.mu.Lock()
	defer tl.mu.Unlock()
	for _, why := range tallyVerdict(tl.cases, env.required) {
		t.Error(why)
	}
}

// tallyVerdict returns why a deep test whose cases ran as cases says
// fails, with required the names required.txt holds, or nothing. Only
// the cases whose every emit stage is required count: such a case that
// declined fails, as declineVerdict fails it, and each required stage
// one of them needs fails when none of them needing it was compared.
// A test none of whose cases is wholly required, as every lowering deep
// test is while only E0 and H are, compares nothing and passes.
//
//nolint:cyclop // two passes over the cases and the stages; split, it reads no plainer
func tallyVerdict(cases []tallyCase, required map[string]bool) []string {
	if !required[emitStageNames[emitE0]] {
		return nil
	}
	var why []string
	var needed, compared [emitStages]int
	for _, c := range cases {
		if !c.needs.required(required) {
			continue
		}
		if !c.compared {
			why = append(why, "a case needing "+c.needs.String()+", every stage required, was declined")
		}
		for s := range emitStages {
			if c.needs.has(s) {
				needed[s]++
				if c.compared {
					compared[s]++
				}
			}
		}
	}
	for s := range emitStages {
		if needed[s] > 0 && compared[s] == 0 {
			why = append(why, emitStageNames[s]+" is required, but no case needing it was compared")
		}
	}
	return why
}

// runEmitDeep writes each of srcs, by file name, to a temporary
// directory and runs it as the stage named stageName runs its cases,
// with each of ports as the port, in parallel, noting each in a tally
// that fails t when cases whose every emit stage is required compared
// nothing (see tallyVerdict). A case the port declines goes through
// declined, which fails one whose every stage is required.
func runEmitDeep(t *testing.T, stageName string, srcs map[string]string, ports ...string) {
	t.Helper()
	st := stageNamed(stageName)
	if st == nil {
		t.Fatalf("no %s stage", stageName)
	}
	dir := t.TempDir()
	var tally emitTally
	t.Run("cases", func(t *testing.T) {
		for file, src := range srcs {
			t.Run(file, func(t *testing.T) {
				t.Parallel()
				sub := filepath.Join(dir, strings.TrimSuffix(file, ".tck"))
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sub, file), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				in := input{name: file, dir: sub, file: file}
				// Deferred, so that a case the port declines, which skips,
				// is noted too, as declined.
				defer func() { tally.note(t, *st, in, !t.Skipped()) }()
				for _, port := range ports {
					if st.run != nil {
						st.run(t, *st, in, port)
					} else {
						runCaseWith(t, *st, in, port)
					}
				}
			})
		}
	})
	tally.check(t)
}

// TestEmitDeep runs --emit, compared as emit-single compares its cases,
// on each of emitNests nested as deeply as the parser allows and each of
// emitChains chainLength links long, the port's stack held to deepStack
// KB, as every run of it is: a port whose backend costs more stack for
// each level, or a frame a link, fails here. A case the port declines
// skips, unless every emit stage it needs is required, and the test
// fails when its cases whose every stage is required compared nothing
// (see tallyVerdict). It runs only as TestPort does.
func TestEmitDeep(t *testing.T) {
	ready(t)
	srcs := map[string]string{}
	for name, nest := range emitNests {
		srcs["nest-"+name+".tck"] = nest(deepLimit - 4)
	}
	for name, chain := range emitChains {
		srcs["chain-"+name+".tck"] = chain(chainLength)
	}
	runEmitDeep(t, emitSingleStage, srcs, env.portRun)
}

// TestEmitTypeChains runs each of typeChains typeChainLength links long
// through --emit, compared as emit-single compares its cases, and the
// header's through --emit-header too, compared as emit-header compares
// its cases, the port's stack held to deepStack KB. It runs only as
// TestPort does.
func TestEmitTypeChains(t *testing.T) {
	ready(t)
	srcs := map[string]string{}
	for name, chain := range typeChains {
		srcs["types-"+name+".tck"] = chain(typeChainLength)
	}
	t.Run("emit", func(t *testing.T) { runEmitDeep(t, emitSingleStage, srcs, env.portRun) })
	t.Run("header", func(t *testing.T) {
		runEmitDeep(
			t,
			emitHeaderStage,
			map[string]string{"types-header.tck": srcs["types-header.tck"]},
			env.portRun,
		)
	})
}

// emitPairRoutes are routes of the bool family of nestFamilies that
// only TestEmitDeepPairs nests, each with itself and with every route of
// the family, both ways round: a closure holding one that captures a
// local of the outer closure's and a parameter of h through both, so
// that each level's inner closure reaches h's variable through the
// pointers every closure around it holds; and a closure passed to a
// scope parameter of a generic function, whose instance calls it.
// emitPairDecls declares what they call.
var emitPairRoutes = map[string]nestRoute{
	"capture-closure": {
		"sc(func() (bool, error) {\nk$ := t.n\nreturn sc(func() (bool, error) {\nreturn k$ >= t.n && %s, noerror\n})\n})?",
		6,
	},
	"generic-closure": {"gsc(t.n, func() (bool, error) {\nreturn %s, noerror\n})?", 3},
}

// emitPairDecls is what emitPairRoutes call beside what the bool family
// declares: gsc, a generic function taking a scope function it calls.
const emitPairDecls = "func gsc[V Data](own x V, scope f func() (bool, error)) (bool, error) {\n" +
	"    return f()\n}\n\n"

// TestEmitDeepPairs runs --emit, compared as emit-single compares its
// cases, on every program in which TestCheckDeepPairs nests a pair of
// the routes of nestFamilies and Go accepts, every one in which it
// would nest a pair of the bool family's routes one of which is of
// emitPairRoutes, and every one in which TestCheckDeepStmts nests a
// pair of statements, or a statement around a value, and Go accepts,
// each as deeply as those tests nest it, the port's stack held to
// deepStack KB and again to pairStack KB: every level of every construct
// must cost the port's backend less than pairStack KB over the nesting
// limit. Each program runs twice: whole, with the generic functions and
// the functions taking scope functions every family declares, which the
// backend lowers whether or not the routes call them, so that the
// stack's routes through instances and closures are covered at both
// stacks; and pruned of what it declares and never uses (see pruneEmit),
// which needs only the backend stages its routes use. It runs only as
// TestPort does.
//
//nolint:cyclop // a nest of loops over the families and their routes, read straight down
func TestEmitDeepPairs(t *testing.T) {
	ready(t)
	srcs := map[string]string{}
	add := func(name, src, params, args string) {
		srcs[name+".tck"] = pruneEmit(src, params, args)
		srcs[name+"-whole.tck"] = src
	}
	for i, fam := range nestFamilies {
		pf := pairFamily{
			decls:  fam.decls,
			leaf:   fam.leaf,
			params: fam.params,
			args:   fam.args,
			owned:  fam.owned,
		}
		routes := fam.routes
		if i == 0 {
			pf.decls += emitPairDecls
			routes = make(map[string]nestRoute, len(fam.routes)+len(emitPairRoutes))
			for n, r := range fam.routes {
				routes[n] = r
			}
			for n, r := range emitPairRoutes {
				routes[n] = r
			}
		}
		for an, a := range routes {
			for bn, b := range routes {
				if a.level == 0 && b.level == 0 ||
					reportingRoutes[an] || reportingRoutes[bn] || reportingPairs[[2]string{an, bn}] {
					continue
				}
				_, newA := emitPairRoutes[an]
				_, newB := emitPairRoutes[bn]
				src := pairProgram(pf, an, bn, a, b, fam.leaf, true)
				if i == 0 && !newA && !newB {
					// The bool family's own pairs are the programs
					// TestCheckDeepPairs checks, without gsc.
					src = pairProgram(pairFamily{
						decls: fam.decls, leaf: fam.leaf, params: fam.params, args: fam.args, owned: fam.owned,
					}, an, bn, a, b, fam.leaf, true)
				}
				add(an+"-"+bn, src, fam.params, fam.args)
			}
		}
	}
	for name, body := range emitStmtNests() {
		add(name, stmtProgram(body, true), nestFamilies[0].params, nestFamilies[0].args)
	}
	runEmitDeep(t, emitSingleStage, srcs, env.portRun, env.portPairs)
}

// pruneEmit returns src, a program of TestEmitDeepPairs, without what it
// declares and never uses: each top-level declaration but h and main
// whose name no other declaration kept names, and h's function value
// parameters, params, with the arguments main passes them, args, when h
// calls neither. Every family declares a generic function and one that
// takes a scope function, which the backend lowers as E4 does, so
// without pruning every pair needs E4, and none of E1 to E3 could be
// compared until it is ported; pruned, a pair needs only the stages of
// what its routes use.
func pruneEmit(src, params, args string) string {
	if params != "" && !strings.Contains(src, "pf(") && !strings.Contains(src, "pq(") {
		src = strings.Replace(src, params+")", ")", 1)
		src = strings.Replace(src, args+")", ")", 1)
	}
	chunks := strings.Split(src, "\n\n")
	name := func(c string) string {
		rest, ok := strings.CutPrefix(c, "type ")
		if !ok {
			rest, ok = strings.CutPrefix(c, "func ")
			if !ok {
				return ""
			}
			if strings.HasPrefix(rest, "(") {
				if i := strings.Index(rest, ") "); i >= 0 {
					rest = rest[i+2:]
				}
			}
		}
		end := strings.IndexFunc(rest, func(r rune) bool {
			return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
		})
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	keep := make([]bool, len(chunks))
	for i, c := range chunks {
		n := name(c)
		keep[i] = n == "" || n == "h" || n == "main" || n == "fix"
	}
	for changed := true; changed; {
		changed = false
		var used strings.Builder
		for i, c := range chunks {
			if keep[i] {
				used.WriteString(c)
				used.WriteString("\n")
			}
		}
		text := used.String()
		for i, c := range chunks {
			if keep[i] {
				continue
			}
			if namesWord(text, name(c)) {
				keep[i], changed = true, true
			}
		}
	}
	var out []string
	for i, c := range chunks {
		if keep[i] {
			out = append(out, c)
		}
	}
	return strings.Join(out, "\n\n")
}

// namesWord reports whether text holds word whole, not as part of a
// longer identifier.
func namesWord(text, word string) bool {
	isIdent := func(b byte) bool {
		return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
	}
	for i := 0; ; {
		k := strings.Index(text[i:], word)
		if k < 0 {
			return false
		}
		start, end := i+k, i+k+len(word)
		if (start == 0 || !isIdent(text[start-1])) && (end == len(text) || !isIdent(text[end])) {
			return true
		}
		i = start + 1
	}
}

// emitStmtNests returns the bodies TestCheckDeepStmts nests in its clean
// programs, by name: every ordered pair of stmtRoutes but those holding
// a defer in a defer, around a local, and every statement route around a
// value nested in each bool route that takes a level, as deeply as it
// nests them.
func emitStmtNests() map[string]string {
	out := map[string]string{}
	for an, a := range stmtRoutes {
		for bn, b := range stmtRoutes {
			if an == "defer" || bn == "defer" {
				continue
			}
			out["stmt-"+an+"-"+bn] = stmtNest(a, b, stmtLevels(an, bn), "x := 1\nif x == 1 {\n}\n")
		}
	}
	addValueNests(out)
	return out
}

// stmtLevels returns how deeply TestCheckDeepStmts nests the pair of
// statement routes an and bn: as deeply as the parser allows, an eighth
// as deeply holding a probed loop, a quarter holding a counting loop or
// a loop beside a fact its writes end.
func stmtLevels(an, bn string) int {
	levels := deepLimit - 6
	switch {
	case probedRoutes[an] || probedRoutes[bn]:
		levels /= 8
	case an == "counting" || bn == "counting" ||
		loopRoutes[an] && factRoutes[bn] || factRoutes[an] && loopRoutes[bn]:
		levels /= 4
	}
	return levels
}

// addValueNests adds to out every statement route but defer nested
// half as deeply as the parser allows around a value nested the other
// half in each bool route that takes a level, as TestCheckDeepStmts
// nests them.
func addValueNests(out map[string]string) {
	half := (deepLimit - 6) / 2
	for sn, s := range stmtRoutes {
		if sn == "defer" {
			continue
		}
		for en, e := range nestFamilies[0].routes {
			if e.level == 0 {
				continue
			}
			levels, depth := half, half
			if probedRoutes[sn] || sn == "counting" {
				levels /= 4
			}
			if probedRoutes[en] {
				depth /= 4
			}
			value := pairNest(e, e, "true", depth)
			out["stmt-"+sn+"-"+en] = stmtNest(s, s, levels, "x := "+value+"\nif x {\n}\n")
		}
	}
}

// TestEmitDeepPortOnly runs what Go's backend is too slow for, the
// chains of typeChains chainLength links long, on the port alone, as
// TestCheckDeepPortOnly does: with its stack held to deepStack KB it
// must exit and write exactly what it does with bigStack KB, and never
// be killed. A chain the port declines skips, unless every emit stage
// its typeChainLength twin, which covers the same blocks of Go's
// backend, needs is required. It runs only as TestPort does.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func TestEmitDeepPortOnly(t *testing.T) {
	ready(t)
	st := stageNamed(emitSingleStage)
	if st == nil {
		t.Fatal("no emit-single stage")
	}
	dir := t.TempDir()
	big, err := writeStackWrapper(dir, "port-big", bigStack)
	if err != nil {
		t.Fatal(err)
	}
	for name, chain := range typeChains {
		file := "types-" + name + ".tck"
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			sub := filepath.Join(dir, name)
			twinDir := filepath.Join(dir, name+"-twin")
			for d, src := range map[string]string{sub: chain(chainLength), twinDir: chain(typeChainLength)} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(d, file), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			in := input{name: file, dir: sub, file: file}
			args := st.args(in)
			got := runCompiler(t, env.portRun, env.portCache, in, args)
			if got.notImplemented() {
				declined(t, *st, input{name: file, dir: twinDir, file: file}, got)
			}
			want := runCompiler(t, big, env.portCache, in, args)
			if got.code != want.code || !bytes.Equal(got.stdout, want.stdout) ||
				!bytes.Equal(got.stderr, want.stderr) {
				t.Errorf(
					"at %d KB the port exited %d, writing\n%s\nat %d KB it exited %d, writing\n%s",
					deepStack,
					got.code,
					got.stderr,
					bigStack,
					want.code,
					want.stderr,
				)
			}
			if want.code > 2 {
				t.Errorf("the port exited %d: %s", want.code, want.stderr)
			}
		})
	}
}
