# Planned builtins

None of this is implemented. It is kept here so the intended shape of the
builtin package is not lost, and kept out of `builtin.tck` so that nothing in
that file is a claim the compiler cannot honour. The compiler reads every
`.tck` file in this directory and takes each extern func there as a builtin,
so those files have to stay true and declare nothing else; this one is not
read.

```tack
package builtin

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
| `...any`                  | a builtin taking variadic or `any` parameters: `any` and `args ...any` exist, but a builtin may have neither, since a call to one passes each argument as itself |
| `write`                   | exposing the file descriptor, which no builtin takes today        |
| `println`                 | the above, plus string concatenation                              |

`append` and `reserve` are no longer planned here: they are generic
builtins, `err := append(mut l, a, b)`, `err := append(mut l, v...)` and
`err := reserve(mut l, n)`, on a `list[T]`, described in `builtin.tck`.
Still planned for lists: `make(list[T], alloc, n)`, which reserves room
for `n` elements and can fail, `clone(l, alloc)`, `truncate(mut l, n)`,
and allocators other than `c_allocator`, an arena and a testing one
among them.

`printf` is no longer planned here: it is `std/fmt`'s
`printf(format string, args ...any)`, which writes to standard output, as
`print` does, rather than to standard error.
