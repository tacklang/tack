# Warn

A warn example that passes.

```tack warn
package main

func main() {
    x := 1
}
```

```diagnostics
main.tck:4:5: warning: declared and not used: x
```

A warn example whose program also errors.

```tack warn
package main

func main() {
    x := 1
    y := z
}
```

```diagnostics
main.tck:4:5: warning: declared and not used: x
main.tck:5:10: error: undefined: z
```
