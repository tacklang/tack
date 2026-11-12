# 2. Source and lexical elements

This chapter describes source text and how it becomes tokens: the
encoding and the bytes a file may hold, line endings, comments,
automatic semicolons, identifiers, the keywords and reserved names,
operators and punctuation, and the spelling of every literal (integer,
float, string, raw string and character literals, with their escapes).
It also holds the nesting limit, since it bounds the source's shape.
What an integer, float or character literal's value is and what type it
takes belong in [Constants and arithmetic](03-arithmetic.md); what a
string or raw string literal's value is belongs in [Types](04-types.md),
with strings; what a name refers to belongs in [Declarations and
scope](05-declarations.md).

## Source text

A source file is a sequence of bytes. The language reads bytes, not
characters: no encoding is decoded, and every rule below is stated in
bytes. Outside comments, string literals, raw string literals and
character literals, a file holds only tokens, the bytes that separate
them, and newlines. The bytes that separate tokens are spaces (0x20)
and horizontal tabs (0x09). Any other byte there, every byte from 0x80
up included, is an error, `invalid character`, reported once for each
such byte, and is otherwise skipped. A byte order mark is three such
bytes. Inside a comment or a literal, a byte is constrained only by the
rules of that comment or literal below.

The following productions name the bytes the lexical grammar uses:

```text
newline       = /* the byte 0x0A, or the bytes 0x0D 0x0A */ .
letter        = "A" ... "Z" | "a" ... "z" | "_" .
decimal_digit = "0" ... "9" .
binary_digit  = "0" | "1" .
octal_digit   = "0" ... "7" .
hex_digit     = "0" ... "9" | "A" ... "F" | "a" ... "f" .
```

```tack error
package main

func main() {
    x := 1 # 2
}
```

```diagnostics
main.tck:4:12: error: invalid character #
```

## Line endings

A newline is a line feed (0x0A), or a carriage return followed by a line
feed (0x0D 0x0A). The two are the same everywhere: a file saved with
either is the same program, and a diagnostic gives the same line and
column in both. A newline ends a line, a line comment, and an
unterminated string or character literal. A carriage return not
followed by a line feed is not a newline: outside a comment or a literal
it is an invalid character, and inside a string literal or a character
literal it is a byte of the literal like any other. A raw string
literal's value leaves out every carriage return (see [Raw string
literals](#raw-string-literals)).

## Comments

There are two forms of comment:

1. A line comment starts with `//` and stops at the end of the line. It
   may be written wherever a line can end: before the package clause,
   between declarations, after a statement, or inside an expression,
   argument list or literal that spans lines. It means the same as the
   newline that ends it.
2. A block comment starts with `/*` and stops at the `*/` that closes
   it. Block comments nest: each `/*` inside a block comment opens one
   that needs its own `*/`, so code that holds a block comment can be
   commented out whole. A block comment that spans a newline means the
   same as a newline; one on a single line means the same as a space.

A comment cannot start inside a string, raw string or character
literal. `//` inside a block comment and `/*` inside a line comment are
text of the comment. A block comment not closed by the end of the file
is an error, `block comment not terminated`.

Successive line comments, each on its own line, with no blank line
between them, form one comment, however each is indented. Comments
carry no meaning in the program.

```tack run exit=7
// A line comment may come before the package clause.
package main

/* Block comments nest: /* this inner one */ is inside the outer one. */
func main() {
    var x int32 = 1 /* a block comment on one line is a space */ + 2 +
        4 // the line ends after +, so it goes on to the next one
    exit(x)
}
```

A block comment that spans a line break ends the statement before it,
as a newline does:

```tack error
package main

func main() {
    x := 1 /* spans
    a line */ + 2
}
```

```diagnostics
main.tck:5:15: error: expected 'statement' found '+'
```

## Semicolons

The grammar uses semicolons, `;`, to end statements and declarations.
A program may write them, but most are left out, as in Go, by this
rule: at a newline, at the start of a line comment, at the start of a
block comment that spans a newline, and at the end of the file, a
semicolon is inserted when the last token before it, comments aside,
is

- an identifier;
- an integer, float, string, raw string or character literal;
- one of the keywords `break`, `continue`, `return`, `unreachable`,
  `fallthrough`, `true`, `false`, `noerror` and `c_allocator`;
- one of the operators `++`, `--` and `?`; or
- one of the closing brackets `)`, `]` and `}`.

An inserted semicolon covers no bytes of the source. So a line that ends
with an operator, a comma or an opening bracket goes on to the next
line, and a line that ends with an operand does not:

```tack error
package main

func main() {
    var x int32 = 3
        + 4
    exit(x)
}
```

```diagnostics
main.tck:5:9: error: expected 'statement' found '+'
```

## Identifiers

An identifier names a program entity, such as a variable, a function,
a type or a field. It is a sequence of one or more ASCII letters,
digits and underscores, starting with a letter or an underscore:

```text
identifier = letter { letter | decimal_digit } .
```

A sequence that spells a keyword is the keyword, not an identifier. A
letter outside ASCII is not a letter: its bytes are invalid characters.
An identifier extends as far as the bytes allow, so `a1_b` is one
identifier. `_` on its own spells an identifier but names nothing; what
it may stand for is in [Declarations and scope](05-declarations.md).

## Keywords and reserved names

The following 40 keywords are reserved and may not be used as
identifiers:

```text
arena        default      for        lib      pub     true
async        defer        from       list     range   type
break        else         func       map      return  union
c_allocator  ensures      goto       mut      scope   unreachable
case         extern       if         noerror  select  var
const        fallthrough  import     own      struct
continue     false        interface  package  switch
```

Each is reserved everywhere, whether or not the grammar uses it at the
place it is written, and some keywords are reserved though no construct
of the language uses them yet. `asm` is not a keyword.

The names of the builtin types are not keywords but identifiers, and
are reserved: none may be declared, read or assigned as a name, nor used
as the name of a struct, a field or a method; they are written only as
types and in conversions. They are

```text
int8     int16    int32    int64    unsafe_size_t      float32   string   any
uint8    uint16   uint32   uint64   unsafe_uintptr_t   float64   bool     error
Allocator
```

and `id` and `span`, which are written only with an element type,
`id[T]` and `span[T]` (see [Types](04-types.md#arenas-ids-and-spans)).

[Types](04-types.md) describes what each names, and [Declarations and
scope](05-declarations.md) which other names may not be declared where.

```tack error
package main

func main() {
    select := 1
    string := "s"
}
```

```diagnostics
main.tck:4:5: error: expected 'statement' found 'select'
main.tck:5:5: error: cannot use string as a name; it is a type
```

## Operators and punctuation

The following byte sequences are operators, assignment operators and
punctuation:

```text
+    &    +=   &=   &&   ==   !=   (    )
-    |    -=   |=   ||   <    <=   [    ]
*    ^    *=   ^=   <-   >    >=   {    }
/    <<   /=   <<=  ++   =    :=   ,    ;
%    >>   %=   >>=  --   !    ...  .    :
     &^        &^=                 ~
+%   -%   *%   +%=  -%=  *%=  ?
```

At each position the longest sequence that is a token is taken, so
`a<<=b` is `a`, `<<=` and `b`, `x---y` is `x`, `--`, `-` and `y`, and
`a+%=b` is `a`, `+%=` and `b`. A
period followed by a decimal digit starts a float literal, not a period.
Every token above is lexed; which of them the grammar uses, and where,
is in [Expressions](06-expressions.md) and [Statements](07-statements.md),
and a token used nowhere is a syntax error where it is written.

## Integer literals

An integer literal is a sequence of digits representing an integer
constant. A prefix sets a base other than decimal: `0b` or `0B` for
binary, `0o` or `0O` for octal, and `0x` or `0X` for hexadecimal, whose
digits `a` through `f` may be in either case. An underscore may stand
between two digits, or between the prefix and the first digit, to group
them; it does not change the value.

```text
int_lit        = decimal_lit | binary_lit | octal_lit | hex_lit .
decimal_lit    = "0" | ( "1" ... "9" ) [ [ "_" ] decimal_digits ] .
binary_lit     = "0" ( "b" | "B" ) [ "_" ] binary_digits .
octal_lit      = "0" ( "o" | "O" ) [ "_" ] octal_digits .
hex_lit        = "0" ( "x" | "X" ) [ "_" ] hex_digits .

decimal_digits = decimal_digit { [ "_" ] decimal_digit } .
binary_digits  = binary_digit { [ "_" ] binary_digit } .
octal_digits   = octal_digit { [ "_" ] octal_digit } .
hex_digits     = hex_digit { [ "_" ] hex_digit } .
```

A decimal literal of more than one digit does not start with `0`:
`0` is a literal, and `007`, `00` and `0_7` are errors, unlike C and
Go, where a leading `0` makes octal. `0o` is the only octal form.

A literal is one token as far as its bytes go: a decimal literal takes
every digit and underscore after its first digit, and a prefixed one
every letter, digit and underscore after its prefix. A literal that
does not match the productions is an error, still one token: a prefix
with no digits, `binary literal 0b has no digits`; a digit its base
does not have, `invalid digit 2 in binary literal 0b102`, so `0x1g` is
one bad literal, not `0x1` and a name; an underscore anywhere the
productions do not allow one, `_ must separate successive digits in
1__0`; and a leading zero. A sign is not part of an integer literal
token: `-5` is two tokens. A `-` before an integer literal makes one
negative literal of the two, which is how the smallest `int64` is
written; that rule, and the range a literal's value must lie in, are
in [Constants and arithmetic](03-arithmetic.md).

```tack run exit=7
package main

func main() {
    var a int32 = 255
    var b int32 = 0xff + 0XF_F
    var c int32 = 0o377 + 0b1111_1111 + 0x_FF
    var d int32 = 1_000 - 0b11_1110_1000
    if b == 2 * a && c == 3 * a && d == 0 {
        exit(7)
    }
}
```

Each literal below is malformed:

```tack error
package main

func main() {
    a := 007
    b := 0x1g
    c := 1__000
    d := 0b
    e := 0b102
}
```

```diagnostics
main.tck:4:10: error: integer literal 007 has a leading zero; write 7
main.tck:5:13: error: invalid digit g in hexadecimal literal 0x1g
main.tck:6:10: error: _ must separate successive digits in 1__000
main.tck:7:10: error: binary literal 0b has no digits
main.tck:8:14: error: invalid digit 2 in binary literal 0b102
```

## Float literals

A float literal is a decimal representation of a floating-point
constant. It has an integer part, a decimal point, a fractional part
and an exponent part, as in Go. The integer and fractional parts are
decimal digits; the exponent part is an `e` or `E` followed by an
optional sign and decimal digits. One of the integer part and the
fractional part may be left out, and one of the decimal point and the
exponent may be left out. An underscore may stand between two digits.

```text
float_lit         = decimal_digits "." [ decimal_digits ] [ decimal_exponent ] |
                    decimal_digits decimal_exponent |
                    "." decimal_digits [ decimal_exponent ] .
decimal_exponent  = ( "e" | "E" ) [ "+" | "-" ] decimal_digits .
```

Unlike a decimal integer literal, a float literal may start with `0`:
`01.5` is a float literal. There are no hexadecimal float literals:
`0x1.8` is the integer literal `0x1` followed by the float literal
`.8`, which the grammar rejects. An exponent with no digits is an
error, `exponent has no digits in 1e`, and an underscore anywhere but
between two digits is the error an integer literal gives. A float
literal's value is in [Constants and arithmetic](03-arithmetic.md).

```tack run exit=0
package main

func main() {
    f := 1.5 + .5 + 1. + 1e1 + 2_5E-1 + 1_0.0e-1
    if f == 16.5 {
        exit(0)
    }
    exit(1)
}
```

Each literal below is malformed:

```tack error
package main

func main() {
    e := 1e
    f := 1_.5
}
```

```diagnostics
main.tck:4:10: error: exponent has no digits in 1e
main.tck:5:10: error: _ must separate successive digits in 1_.5
```

## String literals

A string literal is written between double quotes. Between them, any
byte may appear but a newline, a double quote and a backslash; a
backslash starts an escape sequence, which stands for one byte. The
escapes are those of Go's single-letter ones, the quote, and `\x`
followed by exactly two hexadecimal digits:

```text
string_lit      = `"` { string_byte | escape | `\"` } `"` .
string_byte     = /* any byte but 0x0A, `"` and `\` */ .
escape          = simple_escape | hex_escape .
simple_escape   = `\` ( "a" | "b" | "f" | "n" | "r" | "t" | "v" | `\` ) .
hex_escape      = `\` "x" hex_digit hex_digit .
```

| Escape | Byte | Meaning              |
|--------|------|----------------------|
| `\a`   | 0x07 | alert or bell        |
| `\b`   | 0x08 | backspace            |
| `\f`   | 0x0C | form feed            |
| `\n`   | 0x0A | line feed or newline |
| `\r`   | 0x0D | carriage return      |
| `\t`   | 0x09 | horizontal tab       |
| `\v`   | 0x0B | vertical tab         |
| `\\`   | 0x5C | backslash            |
| `\"`   | 0x22 | double quote         |
| `\xHH` | 0xHH | the byte HH          |

A `\x` takes exactly two hexadecimal digits, in either case, so
`"\x410"` is the bytes `A` and `0`. Any other escape sequence is an
error, `Invalid escape sequence "\\q"`, and so is a `\x` with fewer than
two hexadecimal digits after it. A string literal with no closing quote
before the end of its line is an error, `unterminated string`. A string
literal's value, the bytes it denotes, is in [Types](04-types.md).

## Raw string literals

A raw string literal is written between backquotes, as in Go. Between
them, every byte up to the next backquote is part of the literal, as
written: nothing is an escape, so `` `\n` `` is a backslash and an `n`.
A raw string literal may span lines, and cannot hold a backquote. Its
value leaves out every carriage return, so the value of one that spans
lines does not depend on the line endings the file was saved with.

```text
raw_string_lit = "`" { /* any byte but "`" */ } "`" .
```

A raw string literal with no closing backquote runs to the end of the
file and is an error, `unterminated string`.

```tack run exit=0
package main

func main() {
    s := "quote:\" backslash:\\ hex:\x41\x2a\n"
    r := `raw: \t \x41 "
`
    print(s)
    print(r)
}
```

```output
quote:" backslash:\ hex:A*
raw: \t \x41 "
```

## Character literals

A character literal is one byte written between single quotes: the
byte itself, an escape sequence of the production `escape` above, or
`\'`, the single quote. `\"` is not an escape sequence of a character
literal: `'\"'` is an error, `Invalid escape sequence`.

```text
char_lit  = "'" ( char_byte | escape | `\'` ) "'" .
char_byte = /* any byte but 0x0A, "'" and `\` */ .
```

README.md and docs/tracking.md say a character literal takes every
escape a string literal does, `\"` included; the two descriptions are
not yet reconciled.

A character literal is exactly one byte, so an empty literal, `''`, is
an error, `empty char literal`; one holding more than one byte, `'ab'`,
is `too many characters in char literal`; and one holding a character
whose UTF-8 encoding is longer than one byte is an error that says how
many bytes it is. A quote with nothing after it on its line, `'`, and a
quote and one byte or escape with no closing quote after them, `'a`,
are an error, `unterminated char literal`; a quote and more than one
byte with no closing quote before the end of the line, `'ab`, is `too
many characters in char literal`. A character literal's type
and value are in [Constants and arithmetic](03-arithmetic.md).

```tack run exit=0
package main

func main() {
    var q uint8 = '\''
    var a uint8 = 'A'
    var h uint8 = '\x41'
    var n uint8 = '\n'
    if q == 39 && a == h && n == 10 {
        exit(0)
    }
    exit(1)
}
```

Each literal below, string or character, is malformed:

```tack error
package main

func main() {
    a := "\q"
    b := "\x4"
    c := 'ab'
    d := ''
    e := '\"'
}
```

```diagnostics
main.tck:4:11: error: Invalid escape sequence "\\q"
main.tck:5:11: error: Invalid escape sequence "\\x4"
main.tck:6:10: error: too many characters in char literal
main.tck:7:10: error: empty char literal
main.tck:8:11: error: Invalid escape sequence "\\\""
```

## Nesting limit

A program nests at most 1000 levels deep, counted together across
expressions, statements and types, within the declaration they are in.
Each of these opens one level: a parenthesized expression, a unary
`!`, `-` or `^` and its operand, `mut` and its argument, `own` and its
argument, a call's arguments, an index's or a slice's brackets, a
literal's braces, the block of an `if`, an `else`, a `for` loop, a range
loop or a `defer`, the cases of a `switch`, all together, and an array, view,
list, map or function type. A function's body opens no level, so 1000
parentheses directly in a function's body are within the limit, and a
case's body opens none beyond its switch's. A chain of binary operators binds left
to right and nests no deeper for its length, so `x + x + ... + x` has
no limit, while one nested on the right, `x && (x && (...))`, opens a
level for each parenthesis. An `else if` is a branch of its chain, not a
level, so a chain of any length is flat.

The construct that opens level 1001 is an error, `expression nested too
deeply: more than 1000 levels`, reported once, at the token that opens
it. What it stops is in [Diagnostics](15-diagnostics.md#recovery).
