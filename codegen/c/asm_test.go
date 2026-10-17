package c_test

import "testing"

// asmLocalsProgram uses asm, which C reserves and Tack does not, as the
// name of a parameter, a local, a field and a method, and checks what
// each gives. No package-level name is asm, since a local may not shadow
// one.
const asmLocalsProgram = `package main

type Box struct {
    asm int64
}

type Twice struct {
    n int64
}

func (t Twice) asm() int64 {
    return t.n * 2
}

func inc(asm int64) int64 {
    return asm + 1
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func main() {
    asm := 5
    check(asm == 5, "a local named asm")
    check(inc(1) == 2, "a parameter named asm")
    b := Box{asm: 21}
    check(b.asm == 21, "a field named asm")
    t := Twice{n: 21}
    check(t.asm() == 42, "a method named asm")
}
`

// asmFuncProgram declares a function named asm, whose C name is mangled,
// so C's asm keyword never meets it, and calls it.
const asmFuncProgram = `package main

func asm(n int64) int64 {
    return n * 3
}

func main() {
    print("a function named asm")
    if asm(14) == 42 {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}
`

// asmTypeProgram declares a type named asm and makes and reads one.
const asmTypeProgram = `package main

type asm struct {
    n int64
}

func main() {
    a := asm{n: 7}
    print("a type named asm")
    if a.n == 7 {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}
`

// TestAsmIsAName builds asmLocalsProgram, asmFuncProgram and
// asmTypeProgram, with clang's warnings as errors, and checks every check
// passes: asm is an ordinary Tack name, and the C the backend writes for
// it compiles, since every Tack name it writes is mangled, a field's and
// a method's included.
func TestAsmIsAName(t *testing.T) {
	runChecks(t, buildWithStd(t, asmLocalsProgram), 4)
	runChecks(t, buildWithStd(t, asmFuncProgram), 1)
	runChecks(t, buildWithStd(t, asmTypeProgram), 1)
}
