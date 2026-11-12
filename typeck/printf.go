package typeck

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// std/fmt's printf, eprintf, its twin for standard error, and appendf,
// which appends to a list[uint8] after it, take a format and ...any (see
// formatFuncs). When the format is a string
// the checker knows (see knownString), a literal or an immutable variable
// holding one, every verb in it is checked against the value it takes, as
// Go's vet checks fmt.Printf and clang's -Wformat checks C's printf, and
// each mistake is an error: a verb that does not take its value's type,
// an unknown verb, a % that ends the format, and more or fewer values
// than verbs. A value that is itself an any, passed on, holds a type
// known only at run time and is not checked against its verb, and a call
// that spreads a view, printf(f, args...), passes a number of values known
// only at run time, so only its verbs are checked. A format the checker
// does not know is not checked at all: printf's C, and appendf's Tack,
// write each mistake into the output instead, as Go's fmt does.
//
// The format is read exactly as printf's C reads it, byte by byte: a %
// starts a verb, and the verb is the UTF-8 sequence after it, whose
// length its first byte gives (see verbLen); %% writes a %, and takes no
// value. Each other verb takes the next value: %d and %x an integer of
// any type, %s a string or a []uint8, whose bytes it writes as a
// string's, %t a bool, %c a uint8 and %v any of those. No verb takes a
// view of any other type, though an any can hold one: printf's C writes
// %!v([]int64) for it, as for any verb given a value it does not take.

// stdFmt is the import path of the standard library's fmt package, whose
// formatFuncs the checker checks the format of.
const stdFmt = "std/fmt"

// formatFuncs are the functions of std/fmt that take a printf format and
// its values, each checked the same way, mapped to the index of the
// format among their parameters: the values are every argument after
// it. printf and eprintf take the format first, and appendf takes it
// after the list it appends to. A message about a call names the
// function called, so a mistake in an eprintf format reads "eprintf verb
// %d does not take string".
var formatFuncs = map[string]int{"printf": 0, "eprintf": 0, "appendf": 1}

// checkPrintf checks call, a call to fn, when fn is one of std/fmt's
// formatFuncs and its format is a string the checker knows: see
// checkFormat. Each problem is reported at the value it is about, or at
// the format when it is about the format itself, and a format with more
// values than verbs at the values left over.
func (c *Checker) checkPrintf(call *ast.Call, fn *ast.Func) {
	at, ok := c.formatParam(fn)
	if !ok || len(call.Args) <= at {
		return
	}
	format, ok := c.knownString(call.Args[at])
	if !ok {
		return
	}
	values := call.Args[at+1:]
	var valueTypes []types.Type
	if !call.Spread {
		valueTypes = make([]types.Type, len(values))
		for i, v := range values {
			valueTypes[i] = ast.TypeOf(v)
		}
	}
	for _, p := range checkFormat(fn.Name, format, valueTypes, !call.Spread, c.typeName, c.isBytes) {
		switch {
		case p.value < 0:
			c.errorf(call.Args[at].Spanned(), "%s", p.message)
		default:
			c.errorf(spanOf(values[p.value:p.value+max(p.values, 1)]), "%s", p.message)
		}
	}
}

// isBytes reports whether typ is []uint8, which printf's %s and %v
// write as a string's bytes.
func (c *Checker) isBytes(typ types.Type) bool {
	return c.table.IsView(typ) && c.table.Elem(typ) == types.Uint8
}

// formatParam returns the index of the format among fn's parameters,
// and whether fn is one of std/fmt's formatFuncs.
func (c *Checker) formatParam(fn *ast.Func) (int, bool) {
	if fn == nil || fn.Method {
		return 0, false
	}
	at, ok := formatFuncs[fn.Name]
	pkg := c.declPkg[fn]
	if !ok || pkg == nil || pkg.Path != stdFmt {
		return 0, false
	}
	return at, true
}

// formatProblem is one mistake checkFormat finds: its message, and the
// index of the first value it is about and how many values from there, or
// a value of -1 when it is about the format itself.
type formatProblem struct {
	message string
	value   int
	values  int
}

// checkFormat returns the mistakes in format, a printf format given to
// the function called, whose name each message starts with, with
// values of the types valueTypes, in the order they appear, a mismatch
// between the number of verbs and of values last. When counted is false
// the values are a spread, whose number and types are not known, so only
// the verbs are checked. A value of types.Any, whose type is known only
// at run time, or of types.Invalid, already reported, matches every verb.
// name names a type in a message, and bytes says which type is []uint8.
func checkFormat(
	called string,
	format string,
	valueTypes []types.Type,
	counted bool,
	name func(types.Type) string,
	bytes func(types.Type) bool,
) []formatProblem {
	verbs, lone := formatVerbs(format)
	var problems []formatProblem
	for value, verb := range verbs {
		var typ types.Type // Invalid, matching every verb, when not counted
		if counted && value < len(valueTypes) {
			typ = valueTypes[value]
		}
		if p, bad := verbProblem(called, verb, value, typ, name, bytes); bad {
			problems = append(problems, p)
		}
	}
	if lone {
		problems = append(problems, formatProblem{
			message: called + " format ends in a lone %",
			value:   -1,
		})
	}
	if n := len(verbs); counted && n != len(valueTypes) {
		p := formatProblem{
			message: fmt.Sprintf("%s format has %d %s but %d %s",
				called, n, plural(n, "verb"), len(valueTypes), plural(len(valueTypes), "argument")),
			value: -1,
		}
		if len(valueTypes) > n {
			p.value, p.values = n, len(valueTypes)-n
		}
		problems = append(problems, p)
	}
	return problems
}

// formatVerbs returns the verbs of format, in order, each without its %,
// %% being none, and whether a lone % ends it.
func formatVerbs(format string) (verbs []string, lone bool) {
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		if i == len(format) {
			return verbs, true
		}
		n := verbLen(format[i], len(format)-i)
		if verb := format[i : i+n]; verb != "%" {
			verbs = append(verbs, verb)
		}
		i += n
	}
	return verbs, false
}

// verbProblem returns the mistake in verb, the format's verb number value,
// given a value of type typ in a call to called, which its message names,
// and whether there is one: a verb printf does not know, or one that does
// not take typ. types.Invalid and types.Any match every verb. bytes says
// which type is []uint8.
func verbProblem(
	called string,
	verb string,
	value int,
	typ types.Type,
	name func(types.Type) string,
	bytes func(types.Type) bool,
) (formatProblem, bool) {
	accepts, known := verbTakes(verb, bytes)
	switch {
	case !known:
		return formatProblem{message: "unknown " + called + " verb " + showVerb(verb), value: -1}, true
	case typ == types.Invalid || typ == types.Any || accepts(typ):
		return formatProblem{}, false
	default:
		return formatProblem{
			message: fmt.Sprintf("%s verb %%%s does not take %s", called, verb, name(typ)),
			value:   value,
			values:  1,
		}, true
	}
}

// verbLen returns the length of the verb starting with the byte b, with n
// bytes left in the format: the length of the UTF-8 sequence b starts, 2,
// 3 or 4 for a leading byte and 1 for any other, at most n. The bytes
// after b are not checked, so a verb that is not valid UTF-8 is still
// read whole, as printf's C reads it, and reported as unknown.
func verbLen(b byte, n int) int {
	l := 1
	switch {
	case b >= 0xF8:
	case b >= 0xF0:
		l = 4
	case b >= 0xE0:
		l = 3
	case b >= 0xC0:
		l = 2
	}
	return min(l, n)
}

// verbTakes returns which types the printf verb accepts a value of, and
// whether it is a verb at all: %d and %x an integer, %s a string or a
// []uint8, which bytes says a type is, %t a bool, %c a uint8, %e, %f and
// %g a float, and %v any basic type a value of can be passed as any, or a
// []uint8.
func verbTakes(verb string, bytes func(types.Type) bool) (func(types.Type) bool, bool) {
	switch verb {
	case "d", "x":
		return types.IsInteger, true
	case "s":
		return func(t types.Type) bool { return t == types.String || bytes(t) }, true
	case "t":
		return func(t types.Type) bool { return t == types.Bool }, true
	case "c":
		return func(t types.Type) bool { return t == types.Uint8 }, true
	case "e", "f", "g":
		return types.IsFloat, true
	case "v":
		return func(t types.Type) bool { return types.InAny(t) || bytes(t) }, true
	default:
		return nil, false
	}
}

// showVerb is how a message shows verb, with its %: as written when it is
// valid UTF-8 of graphic characters, and quoted as Go quotes a string
// otherwise, so a newline or a stray byte is visible.
func showVerb(verb string) string {
	if !utf8.ValidString(verb) {
		return strconv.Quote("%" + verb)
	}
	for _, r := range verb {
		if !unicode.IsGraphic(r) || unicode.IsSpace(r) {
			return strconv.Quote("%" + verb)
		}
	}
	return "%" + verb
}
