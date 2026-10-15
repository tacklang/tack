# Planned builtins

None of this is implemented. It is kept here so the intended shape of the
builtin package is not lost, and kept out of `builtin.tck` so that nothing in
that file is a claim the compiler cannot honour. The compiler reads every
`.tck` file in this directory and takes each extern func there as a builtin,
so those files have to stay true and declare nothing else; this one is not
read.

```tack
package builtin

// append appends the given arguments to the given array.
func append(arr []any, args ...any) []any

func write(fd int, adr int, ln int) int

// print concatenates the arguments into a single string and writes
// it to stderr.
func print(args ...any)

// println concatenates the arguments into a single string and writes
// it to stderr with a terminating newline.
func println(args ...any)

```

What each one needs before it could be declared for real:

| Declaration               | Blocked on                                                        |
|---------------------------|-------------------------------------------------------------------|
| `append`, and any `[]any` | growable lists, since `[]T` is only a read-only view parameter |
| `...any`                  | a builtin taking variadic or `any` parameters: `any` and `args ...any` exist, but a builtin may have neither, since a call to one passes each argument as itself |
| `write`                   | exposing the file descriptor, which no builtin takes today        |
| `println`                 | the above, plus string concatenation                              |

`printf` is no longer planned here: it is `std/fmt`'s
`printf(format string, args ...any)`, which writes to standard output, as
`print` does, rather than to standard error.
