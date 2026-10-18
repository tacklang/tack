# Planned builtins

None of this is implemented. It is kept here so the intended shape of the
builtin package is not lost, and kept out of `builtin.tck` so that nothing in
that file is a claim the compiler cannot honour: `builtin.tck` documents
exactly the builtins the compiler implements, and a test keeps the two one to
one.

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

`append`, `reserve`, `pop` and `remove` are no longer planned here: they
are generic builtins, `err := append(mut l, a, b)`, `err := append(mut l,
v...)`, `err := reserve(mut l, n)`, `x, ok := pop(mut l)` and `x, ok :=
remove(mut l, i)`, on a `list[T]`, described in `builtin.tck`. Still
planned for lists: `make(list[T], alloc, n)`, which reserves room for `n`
elements and can fail, `clone(l, alloc)`, `truncate(mut l, n)`, a builtin
that moves every element of one list onto another, which spreading a list
of owned elements cannot, and allocators other than `c_allocator`, an
arena and a testing one among them.

`printf` is no longer planned here: it is `std/fmt`'s
`printf(format string, args ...any)`, which writes to standard output, as
`print` does, rather than to standard error.

Writing bytes to standard output no longer waits on `write`: it is
`std/fmt`'s `write(b []uint8)`, which writes a view's bytes exactly, as
`print` writes a string's. The `write` above, taking a file descriptor,
is still planned.
