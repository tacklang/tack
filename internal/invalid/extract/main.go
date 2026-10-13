package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tacklang/tack/internal/invalid"
)

// defaultPath is the archive's path relative to the module root.
const defaultPath = "examples/invalid.txtar"

// main runs extract with the process's arguments and exits with its status.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run writes the program of the case named by args[0] to stdout, reading
// the archive at args[1] if given, and returns the exit status.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 {
		_, _ = fmt.Fprintln(stderr, "usage: extract <case> [archive]")
		return 2
	}
	path := defaultPath
	if len(args) == 2 {
		path = args[1]
	}
	cases, err := invalid.Load(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "extract:", err)
		return 1
	}
	for _, c := range cases {
		if c.Name != args[0] {
			continue
		}
		if _, err := stdout.Write(c.Source); err != nil {
			_, _ = fmt.Fprintln(stderr, "extract:", err)
			return 1
		}
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "extract: %s has no case %q\n", path, args[0])
	return 1
}
