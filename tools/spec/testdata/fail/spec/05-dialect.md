# Dialect

An untagged block.

```tack
package main
```

An unknown kind.

```tack build
package main
```

A bad attribute.

```tack run exit=3x
package main
```

A misspelled tack.

```tak check
package main
```

Two blocks with no text between them.

```tack fragment
x := 1
```

```tack fragment
y := 2
```

An output block after nothing.

```output
hi
```

An error block with no diagnostics block.

```tack error
package main
```

Raw HTML, <div>, in prose.

A block that is never closed.

```text
x
