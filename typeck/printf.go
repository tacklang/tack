package typeck

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// std/fmt's printf takes a format and ...any. When the format is a string
// the checker knows (see knownString), a literal or an immutable variable
// holding one, every verb in it is checked against the value it takes, as
// Go's vet checks fmt.Printf and clang's -Wformat checks C's printf, and
// each mistake is an error: a verb that does not take its value's type,
// an unknown verb, a % that ends the format, and more or fewer values
// than verbs. A value that is itself an any, passed on, holds a type
// known only at run time and is not checked against its verb, and a call
// that spreads a view, printf(f, args...), passes a number of values known
// only at run time, so only its verbs are checked. A format the checker
// does not know is not checked at all: printf's C writes each mistake
// into the output instead, as Go's fmt does.
//
// The format is read exactly as printf's C reads it, byte by byte: a %
// starts a verb, and the verb is the UTF-8 sequence after it, whose
// length its first byte gives (see verbLen); %% writes a %, and takes no
// value. Each other verb takes the next value: %d and %x an integer of
// any type, %s a string, %t a bool, %c a uint8 and %v any of those.

// stdFmt is the import path of the standard library's fmt package, whose
// printf the checker checks the format of.
const stdFmt = "std/fmt"

// checkPrintf checks call, a call to fn, when fn is std/fmt's printf and
// its format is a string the checker knows: see checkFormat. Each problem
// is reported at the value it is about, or at the format when it is about
// the format itself, and a format with more values than verbs at the
// values left over.
func (c *Checker) checkPrintf(call *ast.Call, fn *ast.Func) {
	if !c.isPrintf(fn) || len(call.Args) == 0 {
		return
	}
	format, ok := c.knownString(call.Args[0])
	if !ok {
		return
	}
	values := call.Args[1:]
	var valueTypes []types.Type
	if !call.Spread {
		valueTypes = make([]types.Type, len(values))
		for i, v := range values {
			valueTypes[i] = ast.TypeOf(v)
		}
	}
	for _, p := range checkFormat(format, valueTypes, !call.Spread, c.typeName) {
		switch {
		case p.value < 0:
			c.errorf(call.Args[0].Spanned(), "%s", p.message)
		default:
			c.errorf(spanOf(values[p.value:p.value+max(p.values, 1)]), "%s", p.message)
		}
	}
}

// isPrintf reports whether fn is std/fmt's printf.
func (c *Checker) isPrintf(fn *ast.Func) bool {
	pkg := c.declPkg[fn]
	return fn != nil && !fn.Method && fn.Name == "printf" && pkg != nil && pkg.Path == stdFmt
}

// formatProblem is one mistake checkFormat finds: its message, and the
// index of the first value it is about and how many values from there, or
// a value of -1 when it is about the format itself.
type formatProblem struct {
	message string
	value   int
	values  int
}

// checkFormat returns the mistakes in format, a printf format, given to
// values of the types valueTypes, in the order they appear, a mismatch
// between the number of verbs and of values last. When counted is false
// the values are a spread, whose number and types are not known, so only
// the verbs are checked. A value of types.Any, whose type is known only
// at run time, or of types.Invalid, already reported, matches every verb.
// name names a type in a message.
func checkFormat(
	format string,
	valueTypes []types.Type,
	counted bool,
	name func(types.Type) string,
) []formatProblem {
	verbs, lone := formatVerbs(format)
	var problems []formatProblem
	for value, verb := range verbs {
		var typ types.Type // Invalid, matching every verb, when not counted
		if counted && value < len(valueTypes) {
			typ = valueTypes[value]
		}
		if p, bad := verbProblem(verb, value, typ, name); bad {
			problems = append(problems, p)
		}
	}
	if lone {
		problems = append(problems, formatProblem{
			message: "printf format ends in a lone %",
			value:   -1,
		})
	}
	if n := len(verbs); counted && n != len(valueTypes) {
		p := formatProblem{
			message: fmt.Sprintf("printf format has %d %s but %d %s",
				n, plural(n, "verb"), len(valueTypes), plural(len(valueTypes), "argument")),
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
// given a value of type typ, and whether there is one: a verb printf does
// not know, or one that does not take typ. types.Invalid and types.Any
// match every verb.
func verbProblem(
	verb string,
	value int,
	typ types.Type,
	name func(types.Type) string,
) (formatProblem, bool) {
	accepts, known := verbTakes(verb)
	switch {
	case !known:
		return formatProblem{message: "unknown printf verb " + showVerb(verb), value: -1}, true
	case typ == types.Invalid || typ == types.Any || accepts(typ):
		return formatProblem{}, false
	default:
		return formatProblem{
			message: fmt.Sprintf("printf verb %%%s does not take %s", verb, name(typ)),
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
// whether it is a verb at all: %d and %x an integer, %s a string, %t a
// bool, %c a uint8 and %v any type a value of can be passed as any.
func verbTakes(verb string) (func(types.Type) bool, bool) {
	switch verb {
	case "d", "x":
		return types.IsInteger, true
	case "s":
		return func(t types.Type) bool { return t == types.String }, true
	case "t":
		return func(t types.Type) bool { return t == types.Bool }, true
	case "c":
		return func(t types.Type) bool { return t == types.Uint8 }, true
	case "v":
		return types.InAny, true
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
