package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// treeCase is a file tree TestLoadTrees makes, and the command lines it
// runs both compilers with in it.
type treeCase struct { // make builds the tree in dir, an empty directory, and returns the
	// inputs to run in it, each with dir, or a directory below it, as its
	// working directory.
	make func(t *testing.T, dir string) []input
}

// mainSrc is a main package's file.
const mainSrc = "package main\n\nfunc main() {\n}\n"

// put writes the files of files below dir, each a slash path and its
// content, making the directories they need.
func put(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil { //nolint:gosec // G306: test files
			t.Fatal(err)
		}
	}
}

// link makes a symbolic link at name below dir pointing to target.
func link(t *testing.T, dir, target, name string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

// chmod sets the mode of name below dir, and sets it back to 0755 when
// the test ends, so the temporary directory can be removed.
func chmod(t *testing.T, dir, name string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
}

// in returns an input running in dir with file as its target, named by
// file.
func in(dir, file string) input { return input{name: file, dir: dir, file: file} }

// declining returns in marked as one the port may answer not implemented
// for, even in a required stage (see input.declines).
func declining(in input) input {
	in.declines = true
	return in
}

// treeCases are the trees TestLoadTrees loads: symbolic links to files,
// to directories, to nothing and to themselves where a package's files
// are; odd names, bytes that are not UTF-8, a newline, dots and names
// differing only in case; files and directories that cannot be read or
// searched; a tack.mod that is a directory, one above the current
// directory, and a current directory reached through a symbolic link;
// targets relative to the current directory every way one can be
// written; an import of a file, of a path through a file, of a
// directory through a symbolic link, and of one that cannot be read; a
// C file named like an option; a deep directory and a long chain of
// imports, with a cycle or a lexing error at its end.
var treeCases = map[string]treeCase{
	"links": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/a.tck": mainSrc, "q/r.tck": "package main\n"})
		link(t, dir, "a.tck", "p/b.tck")
		return []input{in(dir, "p")}
	}},
	"link-to-dir": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/a.tck": mainSrc, "q/r.tck": "package main\n"})
		link(t, dir, "../q", "p/d.tck")
		return []input{in(dir, "p")}
	}},
	"link-broken": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/a.tck": mainSrc})
		link(t, dir, "nothing.tck", "p/c.tck")
		return []input{in(dir, "p")}
	}},
	"link-loop": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/a.tck": mainSrc})
		link(t, dir, "z.tck", "p/z.tck")
		return []input{in(dir, "p")}
	}},
	"dir-named-tck": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/a.tck": mainSrc, "p/e.tck/x.tck": "@\n"})
		return []input{in(dir, "p")}
	}},
	"odd-names": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod":       "module m\n",
			"p/\xff\xfe.tck": "package main\n\nvar x = 1 @\n",
			"p/a\nb.tck":     "package main\n",
			"p/A.tck":        "package main\n",
			"p/a.tck":        mainSrc,
			"p/a.b.tck":      "package main\n",
			"p/.h.tck":       "@\n",
			"p/..x.tck":      "@\n",
			"p/x.TCK":        "@\n",
			"p/_test.tck":    "@\n",
			"p/ü.tck":        "package main\n",
		})
		return []input{in(dir, "p"), in(filepath.Join(dir, "p"), ".")}
	}},
	"odd-dir-names": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod":      "module m\n",
			"\xff/a.tck":    "package main\n\nimport x from \"m/Ab\"\nimport y from \"m/a b\"\nimport z from \"m/\\xfe\"\n",
			"Ab/a.tck":      "package ab\n",
			"a b/a.tck":     "package ab\n",
			"\xfe/a.tck":    "package x\n@\n",
			"a.b/main.tck":  mainSrc,
			"main.tck":      mainSrc,
			"case/a.tck":    "package main\n",
			"case/b.tck":    "package Case\n",
			"keyword/a.tck": "package keyword\n",
		})
		return []input{in(dir, "\xff"), in(dir, "a.b"), in(dir, "m/a.b"), in(dir, "m/\xff"), in(dir, "case"), in(dir, "m/case")}
	}},
	"unreadable": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod":   "module m\n",
			"p/a.tck":    "package main\n\nimport u from \"m/u\"\nimport s from \"m/s\"\n",
			"p/b.tck":    "package main\n",
			"u/u.tck":    "package u\n",
			"s/s.tck":    "package s\n",
			"f/main.tck": mainSrc,
		})
		chmod(t, dir, "u", 0)
		chmod(t, dir, "s", 0o644)
		chmod(t, dir, "f/main.tck", 0)
		// u cannot be read and s not searched, which Go and the port both
		// word; p imports s, whose entries the port cannot type, and f's
		// file cannot be read, which std/os cannot say whether its open
		// or its read refused: the port declines those.
		return []input{
			declining(in(dir, "p")), in(dir, "u"), in(dir, "s"), declining(in(dir, "f")),
			declining(in(dir, "f/main.tck")), in(dir, "m/u"), declining(in(dir, "m/s")), declining(in(dir, "m/f")),
		}
	}},
	"tack-mod-dir": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod/x": "", "p/a.tck": mainSrc})
		return []input{in(dir, "p"), in(dir, "m/p")}
	}},
	"tack-mod-above": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "modul m\n", "p/q/a.tck": mainSrc})
		sub := filepath.Join(dir, "p", "q")
		return []input{in(sub, "."), in(sub, "../q"), in(sub, "m/p/q")}
	}},
	"no-tack-mod": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"p/a.tck": mainSrc})
		return []input{in(dir, "p"), in(dir, "m/p")}
	}},
	"linked-cwd": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"real/tack.mod": "module m\nmodule n\n", "real/p/a.tck": mainSrc})
		link(t, dir, "real", "via")
		via := filepath.Join(dir, "via", "p")
		withPWD := input{name: "PWD", dir: via, file: ".", env: []string{"PWD=" + via}}
		badPWD := input{name: "bad PWD", dir: via, file: ".", env: []string{"PWD=" + dir}}
		relPWD := input{name: "relative PWD", dir: via, file: ".", env: []string{"PWD=via/p"}}
		return []input{in(via, "."), withPWD, badPWD, relPWD}
	}},
	"relative-targets": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod": "module m\n", "p/a.tck": mainSrc, "p/b.tck": "package main\n",
			"r/a.tck": mainSrc, "r/b.tck": "package main\n@\n",
		})
		var ins []input
		for _, p := range []string{"p", "r"} {
			sub := filepath.Join(dir, p)
			ins = append(ins,
				in(dir, "./"+p), in(dir, p+"/"), in(dir, p+"/."), in(dir, p+"//"), in(dir, "./"+p+"/../"+p),
				in(sub, "."), in(sub, "../"+p), in(sub, "./"), in(dir, dir+"/"+p), in(dir, "m/"+p),
				in(dir, "m/"+p+"/"), in(dir, "m//"+p), in(dir, "m/./"+p), in(dir, p+"/b.tck"), in(sub, "b.tck"),
				in(dir, p+"/b_test.tck"))
		}
		return append(ins, in(filepath.Join(dir, "p"), ".."), in(dir, "m/q"), in(dir, "n/p"), in(dir, "m"),
			in(dir, "missing.tck"), in(dir, ".missing"))
	}},
	"imports": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod": "module m\n",
			"p/a.tck": "package main\n\nimport f from \"m/f\"\nimport g from \"m/f/g\"\nimport l from \"m/l\"\n" +
				"import d from \"m/d\"\nimport e from \"m/e\"\nimport s from \"std/nothing\"\nimport b from \"std/builtin\"\n" +
				"import x from \"m/x.tck\"\nimport r from \"./r\"\nimport a from \"/abs\"\nimport m from \"m\"\nimport p from \"m/p\"\n" +
				"import q from \"m/q\"\n",
			"f":           "not a directory\n",
			"real/a.tck":  "package l\n",
			"d/-x.c":      "int x;\n",
			"d/d.tck":     "package d\n",
			"e/x.tck":     "package other\n",
			"x.tck/a.tck": "package x\n",
			"q/a.tck":     "package q\n\nimport z from \"m/z\"\n",
			"z/a.tck":     "package z\n@\n",
			"m.tck":       "package m\n",
		})
		link(t, dir, "real", "l")
		return []input{in(dir, "p"), in(dir, "q"), in(dir, "f"), in(dir, "d")}
	}},
	"c-files": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/a.tck": mainSrc + "@\n", "p/-y.c": "", "p/.h.c": "", "q/a.tck": mainSrc + "@\n", "q/a.c": "", "q/b.c/x": ""})
		return []input{in(dir, "p"), in(dir, "q"), in(dir, "p/a.tck"), in(dir, "q/a.tck")}
	}},
	"deep-dirs": {make: func(t *testing.T, dir string) []input {
		deep := strings.Repeat("d/", 300) + "a.tck"
		put(t, dir, map[string]string{"tack.mod": "module m\n", deep: mainSrc + "@\n"})
		return []input{in(dir, filepath.Dir(deep))}
	}},
	"import-chain": {make: func(t *testing.T, dir string) []input {
		files := map[string]string{"tack.mod": "module m\n", "main.tck": "package main\n\nimport p from \"m/p0\"\n"}
		const n = 3000
		for i := range n {
			files[fmt.Sprintf("p%d/a.tck", i)] = fmt.Sprintf("package p%d\n\nimport q from \"m/p%d\"\n", i, i+1)
		}
		files[fmt.Sprintf("p%d/a.tck", n)] = fmt.Sprintf("package p%d\n\nimport q from \"m/p0\"\nimport r from \"m/end\"\n", n)
		files["end/a.tck"] = "package end\n@\n"
		put(t, dir, files)
		return []input{in(dir, ".")}
	}},
	"import-cycle": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod": "module m\n",
			"main.tck": "package main\n\nimport a from \"m/a\"\nimport b from \"m/b\"\nimport c from \"m/c\"\nimport f from \"std/fmt\"\nimport z from \"m/z\"\n",
			"a/a.tck":  "package a\n\nimport b from \"m/b\"\n",
			"b/b.tck":  "package b\n\nimport a from \"m/a\"\nimport c from \"m/c\"\n",
			"c/c.tck":  "package c\n\nimport a from \"m/a\"\nimport m from \"m\"\n",
			"z/z.tck":  "package z\n@\n",
		})
		return []input{in(dir, ".")}
	}},
	"std-module": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{
			"tack.mod":   "module std\n",
			"fmt/a.tck":  "package fmt\n\nimport os from \"std/os\"\nimport x from \"other/x\"\n",
			"os/a.tck":   "package os\n@\n",
			"tool/a.tck": "package main\n\nimport f from \"std/fmt\"\n",
		})
		return []input{in(dir, "tool"), in(dir, "fmt"), in(dir, "std/tool")}
	}},
	"empty-dirs": {make: func(t *testing.T, dir string) []input {
		put(t, dir, map[string]string{"tack.mod": "module m\n", "p/x_test.tck": "", "q/.a.tck": "", "r/a.go": ""})
		return []input{in(dir, "p"), in(dir, "q"), in(dir, "r"), in(dir, "m/p")}
	}},
}

// TestLoadTrees runs each of treeCases' command lines through both
// compilers as the lex-packages, ast-packages and res-packages stages
// do, compared as they compare them, the port with its stack held to
// deepStack KB, as TestDeepNesting runs it. It runs only as TestPort does, with
// TACK_PORT_TEST=1 and -count.
func TestLoadTrees(t *testing.T) {
	ready(t)
	wrapper := env.portRun
	for name, tc := range treeCases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ins := tc.make(t, dir)
			for _, st := range []string{"lex-packages", "ast-packages", "res-packages"} {
				stg := stageNamed(st)
				if stg == nil {
					t.Fatalf("no %s stage", st)
				}
				for _, input := range ins {
					t.Run(st+"/"+input.name, func(t *testing.T) {
						runCaseWith(t, *stg, input, wrapper)
					})
				}
			}
		})
	}
}
