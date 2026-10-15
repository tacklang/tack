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

// printf formats the given arguments C-style and writes to stderr.
func printf(format string, args ...any)

func exit(code int)
```

What each one needs before it could be declared for real:

| Declaration               | Blocked on                                                        |
|---------------------------|-------------------------------------------------------------------|
| `append`, and any `[]any` | growable lists, since `[]T` is only a read-only view parameter, and an `any` type |
| `...any`                  | variadics, which the parser currently rejects, and an `any` type  |
| `write`                   | exposing the file descriptor, which no builtin takes today        |
| `println`, `printf`       | the above, plus string concatenation and formatting               |

`printf` also depends on `std/fmt`, a standard library package; imports and
packages exist, but the standard library has no packages yet.
