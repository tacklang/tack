# The Tack language specification

This is the normative description of the language the Tack compiler
accepts: what a program may say, what it means, and what the compiler
rejects. It records decisions already made, in the compiler and in the
documents below; it does not make them. A question the existing decisions
do not answer is listed in [open-decisions.md](../open-decisions.md), not
settled here, and where this text and the compiler disagree, one of them
has a bug.

The specification is being assembled. Until a chapter is filled in, the
language is described by "The language today" in the
[README](../../README.md), by [safety.md](../safety.md),
[extern.md](../extern.md) and [guarantees.md](../guarantees.md), and by
[builtin/builtin.tck](../../builtin/builtin.tck) for the builtins.

The standard library is outside this specification's scope by choice.
The specification says only where std sits, the module `std`, and how a
program imports it.

## Organization

The specification is organized by topic, as Go's is, one chapter per
file, numbered so their order is stable. Each chapter opens with a scope
statement that says what belongs in it, so chapters do not overlap: a
topic that touches several chapters is described in the one its scope
names, and the others link to it.

1. [Notation](01-notation.md)
2. [Source and lexical elements](02-lexical.md)
3. [Constants and arithmetic](03-arithmetic.md)
4. [Types](04-types.md)
5. [Declarations and scope](05-declarations.md)
6. [Expressions](06-expressions.md)
7. [Statements](07-statements.md)
8. [Ownership and memory](08-ownership.md)
9. [Errors](09-errors.md)
10. [Safety and proofs](10-safety.md)
11. [Generics and interfaces](11-generics.md)
12. [Packages, modules and programs](12-packages.md)
13. [Builtins](13-builtins.md)
14. [Extern C](14-extern.md)
15. [Diagnostics](15-diagnostics.md)

## Examples

Every fenced code block whose info string is `tack` followed by a kind
is a test: `TestSpecExamples`, in `tack/spec_test.go`, checks each one
in `go test ./...`. The word after `tack` says what the block is and
what the compiler must do with it. Each program is one whole file,
checked alone as `main.tck` with `tack --check --max-errors 0`, so
every diagnostic is reported. `--check` checks a file whose package is
not `main` as a library; a `tack run` program must be `package main` to
build.

| Info string        | The block is                     | It must                                                       |
|--------------------|----------------------------------|---------------------------------------------------------------|
| `tack run exit=N`  | a whole program                  | pass `tack --check` with no diagnostic, build with nothing on standard error, and its binary write nothing to standard error and exit with status `N`, 0 to 255 |
| `tack check`       | a whole program                  | be accepted by `tack --check` with no diagnostic at all       |
| `tack error`       | a whole program                  | be rejected with exactly the diagnostics of the next block, at least one an error |
| `tack warn`        | a whole program                  | be accepted with exactly the diagnostics of the next block, at least one a warning |
| `tack fragment`    | part of a program                | nothing: it is not compiled                                   |

A `tack error` or `tack warn` block is followed directly, with nothing but
blank lines between, by a block whose info string is `diagnostics`. It
holds the first line of each diagnostic, error, warning or note, exactly
as the compiler prints it, in the order it prints them, and nothing else:
the source lines the compiler shows under each are left out. The test
compares those first lines with the block, ignores the numbered source
lines and markers under each and the blank lines between diagnostics,
and fails on any other line the compiler prints. For example:

````markdown
```tack error
package main

func half(a int64, b int64) int64 {
    return a / b
}

func main() {
    code, fits := int32(half(8, 2))
    if fits { exit(code) }
}
```

```diagnostics
main.tck:4:16: error: divisor b may be zero; check b != 0 first, or use q, ok := ... / b
```
````

A `tack run` block may be followed in the same way by a block whose info
string is `output`. It holds exactly the bytes the program writes to
standard output, so that output must end in a newline; without the block,
standard output is not checked. A `run` example is checked with
`tack --check` everywhere, and built and run only where `clang` is
installed, as the other tests that build programs are. Within 10
seconds of starting, the binary must exit and its standard output and
standard error must close, which a process it started can keep open.

Prefer a checked example to a fragment: a fragment is not compiled, so
nothing notices when it goes stale. The test logs how many fragments
there are and where (`go test ./tack -run TestSpecExamples -v`).

## The Markdown dialect

So that the `tack` blocks the test finds are exactly the `tack` blocks
a reader sees, the files here are written in a strict dialect of
Markdown. Code a reader sees anywhere else is not checked: in a fenced
block of another info string or of none, or in an indented code block,
which the dialect allows, though no fence can sit in one, since only a
fence may hold three backticks.

- A file holds only printable ASCII, bytes 0x20 to 0x7E, and newlines:
  no tab, no carriage return, no byte past ASCII and no byte order mark.
  A program that needs a byte past ASCII writes it with an escape,
  `\xHH`.
- A fence is a line that starts at column 0 with three or more
  backticks or tildes. On an opening fence, what follows the run is its
  info string, which is either nothing or a lowercase word followed by
  attributes, each one space and then lowercase letters, digits and `=`,
  as in `tack run exit=3`. The word is one of `tack`, `output`,
  `diagnostics`, `text` and `markdown`, so a misspelled `tack`, such as
  `tak` or `tck`, fails rather than going unchecked. A fence opens a
  block even when its info string fails.
- A block ends at the first line that starts at column 0 with a run of
  the same character, at least as long as the one that opened it, with
  nothing after it but spaces, since a renderer ends it there. That line
  must be exactly the opening run. A longer fence can therefore quote a
  shorter one, as above.
- Inside a block, no indented line is nothing but a run of the fence's
  character at least as long as the opening one, since a renderer may
  end the block there.
- Outside a block, only a fence holds three backticks or three tildes,
  so a fence cannot sit in a list item, a block quote or an indented
  code block, and inline code in prose uses one or two backticks.
- Outside a block, a `<` is never followed by a letter, `/`, `!` or `?`,
  anywhere on a line, since that could be raw HTML; prose writes a
  comparison with spaces, as `a < b`.
- A file does not start with a line of `---`, which would start front
  matter, and outside a block no line starts, after spaces, with `$$`,
  which would start a math block on GitHub.
- In this index, each chapter is linked by a list line of exactly this
  form: at column 0, a number of one to nine digits, a dot, one space,
  and `[Title](NN-name.md)`, the title one or more ASCII letters,
  digits, spaces and the punctuation `, . : ; ( ) ' & -`, not holding
  `www.` in any case, and nothing after the closing parenthesis. The
  line is outside every fenced block, and starts the file, follows a
  blank line, or follows another line that counts. Such a line renders
  as a link to its chapter under CommonMark and on GitHub, whose tables,
  footnotes, math and autolinks need characters the title cannot hold,
  or `www.`; any other mention of a chapter file, a link or not,
  counts for nothing.

## What fails the test

`TestSpecExamples` reports each of these, with its file and line:

- the first byte that is not printable ASCII or a newline, after which
  nothing else in the file is read;
- a first line of `---`;
- an opening fence whose info string is not of the dialect's grammar,
  or whose first word is not one the dialect allows;
- a line that ends a block but is not exactly the opening run;
- outside a block, a line other than a fence that holds three backticks
  or three tildes;
- outside a block, a `<` followed by a letter, `/`, `!` or `?`;
- outside a block, a line starting, after spaces, with `$$`;
- inside a block, an indented line that is nothing but a run of the
  fence's character at least as long as the opening one, where a
  renderer may end the block;
- a block that is never closed;
- a `tack` block with no kind, or an unknown one;
- a `tack check`, `tack error`, `tack warn` or `tack fragment` block
  with any attribute;
- a `tack run` block without exactly one attribute, or whose attribute
  is not `exit=N`, or whose `N` is past 255;
- an `output` or `diagnostics` block directly after the `tack` block it
  belongs to, with any attribute;
- any other block directly after a `tack` block, with only blank lines
  between, so even two `tack` blocks in a row need text between them;
- a `diagnostics` or `output` block that does not directly follow a
  `tack` block of a kind that takes it;
- a `tack error` or `tack warn` block without a `diagnostics` block
  directly after it, or whose block holds no line that is an error's
  header, or a warning's, respectively;
- `tack --check` writing anything to standard output, for any example;
- for `tack run`, `tack check`, `tack error` and `tack warn`, the
  program not doing what the table above says it must, a `tack run`
  program running past 10 seconds included;
- a Markdown file in `docs/spec` that cannot be read, and a `docs/spec`
  with no Markdown file at all.

`TestSpecIndex` reports a directory, or a file that is not a `.md` file,
in `docs/spec`; a chapter this page does not link by a list line of the
form above; such a line linking a chapter that does not exist; and a
file other than this page not named `NN-name.md`.

To show a block of Tack without testing it, as above, put it inside a
longer fence of another language.
