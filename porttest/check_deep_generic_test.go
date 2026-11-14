package porttest

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// genericRoute is a statement TestCheckDeepGeneric nests: how it wraps
// the statements it holds, given its level, i, and the type parameter
// a switch on one switches on at that level, param, and gives the type
// parameter the statements it holds may switch on, with the parser's
// nesting levels it takes.
type genericRoute struct {
	wrap  func(i int, param, body string) (string, string)
	level int
}

// genericRoutes are the statements TestCheckDeepGeneric nests: a fields
// loop, a switch on a type parameter whose case list[E] declares the
// type parameter the next such switch inside it is on, a closure passed
// to a scope parameter as a statement, and, to nest them in, an if and
// a range over a count.
var genericRoutes = map[string]genericRoute{
	"fields": {func(i int, param, body string) (string, string) {
		return fmt.Sprintf("for f%d := range fields(P) {\nif f%d.index == 0 {\n}\n%s}\n", i, i, body), param
	}, 1},
	"param-switch": {func(i int, param, body string) (string, string) {
		next := fmt.Sprintf("E%d", i+1)
		return fmt.Sprintf("switch %s {\ncase list[%s]:\n%sdefault:\n}\n", param, next, body), next
	}, 1},
	"closure": {func(_ int, param, body string) (string, string) {
		return "sv(func() {\n" + body + "})\n", param
	}, 3},
	"if": {func(_ int, param, body string) (string, string) {
		return "if b {\n" + body + "}\n", param
	}, 1},
	"range": {func(_ int, param, body string) (string, string) {
		return "for range 3 {\n" + body + "}\n", param
	}, 1},
}

// genericNest nests the routes a and b, alternating, a first from the
// outside, as deeply as limit of the parser's nesting levels allows,
// around inner, the first switch on a type parameter on E0.
func genericNest(a, b genericRoute, limit int, inner string) string {
	var routes []genericRoute
	levels := 0
	for i := 0; ; i++ {
		r := a
		if i%2 == 1 {
			r = b
		}
		if levels+r.level > limit {
			break
		}
		levels += r.level
		routes = append(routes, r)
	}
	// Each switch's parameter is the one the switch outside it declared,
	// so the names are worked out from the outside in, and the nest is
	// built from the inside out.
	params := make([]string, len(routes)+1)
	params[0] = "E0"
	for i, r := range routes {
		_, params[i+1] = r.wrap(i, params[i], "")
	}
	e := inner
	for i := len(routes) - 1; i >= 0; i-- {
		e, _ = routes[i].wrap(i, params[i], e)
	}
	return e
}

// genericProgram returns a program whose generic h, of a type parameter
// E0 constrained by Data, holds body, with a struct P for fields loops
// and a scope parameter for closures, a function of a bool for a value
// passed as one, whose main ends in checkError, or, when clean, calls h
// and reports nothing.
func genericProgram(body string, clean bool) string {
	main := "func main() {\n    " + checkError + "}\n"
	if clean {
		main = "func main() {\n    e := h(true, 1)\n    if e.code != 0 {\n        return\n    }\n}\n"
	}
	return "package main\n\ntype P struct {\n    n int64\n}\n\n" +
		"func sv(scope f func()) {\n    f()\n}\n\n" +
		"func bb(x bool) bool {\n    return x\n}\n\n" +
		"func h[E0 Data](b bool, v E0) error {\n" + body + "    return noerror\n}\n\n" + main
}

// instanceChains are programs TestCheckDeepGeneric checks that chain
// generic functions, n links long, each calling the next with its type
// parameter, which its own parameter moves on, so that every link is an instance found from the one
// before, which Go's instantiate finds from a worklist and the port must
// too: one of instanceLinks links, as many as fit under MaxInstances,
// whose last link is clean or returns a string, and one a link longer
// than MaxInstances allows.
func instanceChain(n int, last string) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := range n {
		fmt.Fprintf(&b, "func g%d[T Data](own x T) T {\n    return g%d(own x)\n}\n\n", i, i+1)
	}
	fmt.Fprintf(&b, "func g%d[T Data](own x T) T {\n    return %s\n}\n\n", n, last)
	b.WriteString("func main() {\n    if g0(1) == 2 {\n        return\n    }\n}\n")
	return b.String()
}

// instanceLinks is how many links TestCheckDeepGeneric's instance chains
// hold: the instances of g0 to g9000, well under MaxInstances.
const instanceLinks = 9000

// TestCheckDeepGeneric nests every ordered pair of genericRoutes,
// alternating, as deeply as the parser allows, around a local's
// declaration and an if on it, in a generic function's body: fields
// loops in fields loops, switches on a type parameter in switches on
// the one the case around them declares, closures in closures, each in
// the others and in an if and a range. Each runs as a valid nest, one
// whose value is of the wrong type, and, for a nest holding switches on
// a type parameter, one for each of narrowedInners, each under --check, compared as
// check-core compares its cases, and --port-types, compared as TestTypes
// compares a case of the type checker's, and one in a program Go
// accepts, under --port-types, compared in the phase "bodies", each with
// the port's stack held to deepStack KB and again to pairStack KB. It
// also runs the instance chains (see instanceChain): one Go accepts,
// under --port-types, one returning a string from its last link and one
// a link too long for MaxInstances, under --check and --port-types. It
// runs only as TestPort does.
func TestCheckDeepGeneric(t *testing.T) {
	ready(t)
	st := stageNamed(checkStageInfo[stageCore].stage)
	if st == nil {
		t.Fatal("no check-core stage")
	}
	dir := t.TempDir()
	type shape struct {
		name, src string
		clean     bool
		// typesDepth is how deeply the nest is written for --port-types,
		// when it is less deep than for --check, of the routes a and b in
		// the variant named.
		typesDepth int
		a, b       genericRoute
		variant    string
	}
	var shapes []shape
	for an, a := range genericRoutes {
		for bn, b := range genericRoutes {
			for _, variant := range []string{"valid", "invalid", "clean"} {
				depth := 0
				if an == "param-switch" || bn == "param-switch" {
					// Each case narrows the type parameter of every switch
					// around it to a list of its own, so the table holds a
					// type for each pair of levels, each named in full:
					// the dump of the whole nest would run to gigabytes.
					depth = paramSwitchTypesDepth
				}
				shapes = append(shapes, shape{an + "-" + bn + "-" + variant, genericSource(a, b, variant, deepLimit-6), variant == "clean", depth, a, b, variant})
			}
			if an != "param-switch" && bn != "param-switch" {
				continue
			}
			for _, variant := range slices.Sorted(maps.Keys(narrowedInners)) {
				shapes = append(shapes, shape{an + "-" + bn + "-" + variant, genericSource(a, b, variant, deepLimit-6), false, paramSwitchTypesDepth, a, b, variant})
			}
		}
	}
	shapes = append(shapes,
		shape{name: "instances-clean", src: instanceChain(instanceLinks, "x"), clean: true},
		shape{name: "instances-invalid", src: instanceChain(instanceLinks, "\"s\"")},
		shape{name: "instances-too-many", src: instanceChain(10001, "x")},
	)
	for _, sh := range shapes {
		file := sh.name + ".tck"
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			if err := os.WriteFile(filepath.Join(dir, file), []byte(sh.src), 0o600); err != nil {
				t.Fatal(err)
			}
			in := input{name: file, dir: dir, file: file}
			types := in
			if sh.typesDepth > 0 {
				typesFile := "types-" + file
				src := genericSource(sh.a, sh.b, sh.variant, sh.typesDepth)
				if err := os.WriteFile(filepath.Join(dir, typesFile), []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
				types = input{name: typesFile, dir: dir, file: typesFile}
			}
			for _, port := range []string{env.portRun, env.portPairs} {
				if sh.clean {
					if sh.typesDepth > 0 {
						// The clean nest's --check is the backend's; only the
						// stack of the whole nest is checked.
						runStackOnly(t, in, port)
					}
					compared, _, phase := checkTypes(t, types, port, true)
					if !compared || phase != "bodies" {
						t.Errorf("not compared in the phase bodies: compared %v, phase %q", compared, phase)
					}
					continue
				}
				runCaseWith(t, *st, in, port)
				checkTypes(t, types, port, false)
			}
		})
	}
}

// narrowedInners are the statements TestCheckDeepGeneric puts innermost
// in a nest holding switches on a type parameter, by variant, each one
// reported with the type of h's parameter v named in full, as the
// innermost case narrowed it, a type as deep as the nest: a comparison
// of mismatched types, arithmetic, a unary operator, a conversion, and
// v passed as another type.
var narrowedInners = map[string]string{
	"typed-mismatch": "if v == \"s\" {\n}\n",
	"typed-arith":    "y := v + 1\nif y == 0 {\n}\n",
	"typed-unary":    "y := -v\nif y == 0 {\n}\n",
	"typed-conv":     "y := int64(v)\nif y == 0 {\n}\n",
	"typed-pass":     "if bb(v) {\n}\n",
}

// paramSwitchTypesDepth is how deeply TestCheckDeepGeneric writes a nest
// holding switches on a type parameter for --port-types.
const paramSwitchTypesDepth = 100

// genericSource returns the program of the nest of the routes a and b,
// as deep as limit allows, in the variant named: valid, invalid, its
// value of the wrong type, clean, in a program Go accepts, or one of
// narrowedInners, whose statement is innermost.
func genericSource(a, b genericRoute, variant string, limit int) string {
	leaf := "1"
	if variant == "invalid" {
		leaf = "\"s\""
	}
	inner, named := narrowedInners[variant]
	if !named {
		inner = "x := " + leaf + "\nif x == 1 {\n}\n"
	}
	body := genericNest(a, b, limit, inner)
	return genericProgram(body, variant == "clean")
}

// runStackOnly runs the port on in, a program Go accepts, under --check
// --max-errors 0, with its stack held as port's wrapper holds it, and
// fails unless it checks the whole program and then answers that the
// backend, which decides the rest, is not implemented: a port that runs
// out of stack checking it fails here.
func runStackOnly(t *testing.T, in input, port string) {
	t.Helper()
	res := runCompiler(t, port, env.portCache, in, []string{"--check", "--max-errors", "0", in.file})
	if !res.notImplemented() {
		t.Errorf("the port exited %d on %s, where it checks the program and declines the backend:\n%s",
			res.code, in.file, res.stderr)
	}
}
