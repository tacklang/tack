# Stand-in

A check example whose --check writes to standard output.

```tack check
// stdout
```

An error example whose --check writes a line that is no part of a
diagnostic, after its one error.

```tack error
// stray
```

```diagnostics
main.tck:1:1: error: undefined: x
```

A run example whose program cannot be started.

```tack run exit=0
// noexec
```
