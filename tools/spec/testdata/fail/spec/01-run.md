# Run

A run example that passes, with its output.

```tack run exit=3
package main

func main() {
    print("hi\n")
    exit(3)
}
```

```output
hi
```

A program that exits with 124 by itself, which is no timeout.

```tack run exit=124
package main

func main() {
    exit(124)
}
```

The wrong output.

```tack run exit=0
package main

func main() {
    print("hi\n")
}
```

```output
bye
```

An extra newline.

```tack run exit=0
package main

func main() {
    print("hi\n\n")
}
```

```output
hi
```

The wrong exit status.

```tack run exit=4
package main

func main() {
    exit(3)
}
```

A write to standard error.

```tack run exit=0
package main

import os from "std/os"

func main() {
    e := os.write_stderr([]uint8("oops\n"))
    if e.code != 0 {
        exit(1)
    }
}
```

A program a signal kills: the shell it starts kills it.

```tack run exit=0
package main

import os from "std/os"
import text from "std/text"

func start() error {
    mut argv := make(list[text.Text], c_allocator)
    for _, a := range [3]string{"sh", "-c", "kill -KILL $PPID"} {
        t := text.from_bytes([]uint8(a), c_allocator)?
        append(mut argv, t)?
    }
    _, e := os.run_inherit([]uint8("sh"), argv, c_allocator)
    return e
}

func main() {
    e := start()
    if e.code != 0 {
        exit(1)
    }
}
```

A program that passes --check but does not build.

```tack run exit=0
package main

extern("spec_nowhere") func nowhere()

func main() {
    nowhere()
}
```

A program that does not compile.

```tack run exit=0
package main

func main() {
    nothing()
}
```

A program that ends at once, leaving a process that holds its standard
output for 30 seconds, which runs past the 10 seconds a run has.

```tack run exit=0
package main

import os from "std/os"
import text from "std/text"

func start() error {
    mut argv := make(list[text.Text], c_allocator)
    for _, a := range [3]string{"sh", "-c", "sleep 30 &"} {
        t := text.from_bytes([]uint8(a), c_allocator)?
        append(mut argv, t)?
    }
    _, e := os.run_inherit([]uint8("sh"), argv, c_allocator)
    return e
}

func main() {
    e := start()
    if e.code != 0 {
        exit(1)
    }
}
```

A program a hangup kills: the shell it starts sends it SIGHUP.

```tack run exit=0
package main

import os from "std/os"
import text from "std/text"

func start() error {
    mut argv := make(list[text.Text], c_allocator)
    for _, a := range [3]string{"sh", "-c", "kill -HUP $PPID"} {
        t := text.from_bytes([]uint8(a), c_allocator)?
        append(mut argv, t)?
    }
    _, e := os.run_inherit([]uint8("sh"), argv, c_allocator)
    return e
}

func main() {
    e := start()
    if e.code != 0 {
        exit(1)
    }
}
```

A program that writes nothing, but puts the output its block expects
in files of its directory named stdout and stderr, where an earlier
launcher wrote what a program wrote: the output it is judged by is
what it wrote, so it fails.

```tack run exit=0
package main

import os from "std/os"

func main() {
    r1 := os.remove_path([]uint8("stdout"))
    r2 := os.remove_path([]uint8("stderr"))
    e := os.write_file([]uint8("stdout"), []uint8("forged\n"))
    e2 := os.write_file([]uint8("stderr"), []uint8(""))
    if e.code != 0 || e2.code != 0 || r1.code < 0 || r2.code < 0 {
        exit(1)
    }
}
```

```output
forged
```

A program that passes, leaving behind, in its process group, a process
that holds none of its output and would run for 30 seconds: the
launcher kills it as the run ends, which make test-spec-tool and
tackc's porttest spec stage check, failing should it outlive the run.

```tack run exit=0
package main

import os from "std/os"
import text from "std/text"

func start() error {
    mut argv := make(list[text.Text], c_allocator)
    for _, a := range [3]string{"sh", "-c", "sleep 30 < /dev/null > /dev/null 2>&1 &"} {
        t := text.from_bytes([]uint8(a), c_allocator)?
        append(mut argv, t)?
    }
    _, e := os.run_inherit([]uint8("sh"), argv, c_allocator)
    return e
}

func main() {
    e := start()
    if e.code != 0 {
        exit(1)
    }
}
```
