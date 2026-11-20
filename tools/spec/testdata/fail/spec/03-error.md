# Error

An error example that passes, with notes.

```tack error
package main

func main() {
    mut d := len(make(list[int64], c_allocator))
    if d != 0 {
        d -%= 1
        code, fits := int32(8 / d)
        if fits { exit(code) }
    }
}
```

```diagnostics
main.tck:7:33: error: divisor d may be zero; check d != 0 first, or use q, ok := ... / d
main.tck:5:8: note: this guard proves d != 0
main.tck:6:9: note: but d is assigned here, so the proof ends
```

The wrong message.

```tack error
package main

func main() {
    x := y
}
```

```diagnostics
main.tck:4:10: error: wrong
```

An error example that compiles.

```tack error
package main

func main() {
}
```

```diagnostics
main.tck:4:10: error: wrong
```

A diagnostics block with a line missing.

```tack error
package main

func main() {
    f1()
    f2()
}
```

```diagnostics
main.tck:4:5: error: undefined: f1
```
