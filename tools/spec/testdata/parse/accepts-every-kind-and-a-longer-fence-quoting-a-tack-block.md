# Title

Use `x` inline, ``a`b`` too, and a < b.

```tack run exit=3
run src
```

```output
hi
```

Text.

```tack check
check src
```

Text.

```tack error
error src
```

```diagnostics
main.tck:1:1: error: x
main.tck:2:1: note: y
```

Text.

~~~tack warn
warn src
~~~

~~~diagnostics
main.tck:1:1: warning: x
~~~

Text.

```tack fragment
x := 1
```

Text.

```tack run exit=0
quiet
```

Text.

````markdown
```tack error
not checked
```
````
