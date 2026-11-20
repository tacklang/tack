# Check

A check example that passes.

```tack check
package main

func main() {
}
```

A check example that warns.

```tack check
package main

func main() {
    x := 1
}
```

A check example that does not compile.

```tack check
package main

func main() {
    nothing()
}
```
