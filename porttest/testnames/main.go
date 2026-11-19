// Command testnames writes porttest/testdata/gotest-names.txt: every
// test and subtest go test runs in the packages whose runtime
// assertions are moving to the port's run records, one per line, the
// package's directory below the module's root, a tab and the name, in
// byte order. porttest's TestRunRecordsCoverage checks every record's
// replaces line against it, and checks its top-level tests against the
// test functions of those packages, so a list gone stale fails there.
//
// It runs go test -json -count=1 over the packages, from the module's
// root, the directory above the one it is run in, as go generate runs
// it in porttest; a skipped test is listed too, since go test still names
// it. It writes nothing unless go test passes, so a run cut short never
// leaves a partial list. It takes as long as those tests take.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// module is this module's path, which go test's package names start
// with.
const module = "github.com/tacklang/tack/"

// packages are the packages whose tests are listed, by directory.
var packages = []string{"codegen/c", "tack", "test", "internal/progen"}

// event is the part of one go test -json event testnames reads.
type event struct {
	Action  string
	Package string
	Test    string
}

// main writes the list, exiting 1 with a message when go test cannot be
// run or its output cannot be read.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "testnames:", err)
		os.Exit(1)
	}
}

// run does main's work and returns what stopped it.
func run() error {
	args := []string{"test", "-json", "-count=1", "-timeout", "60m"}
	for _, p := range packages {
		args = append(args, "./"+p)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = ".."
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	seen := map[string]bool{}
	sc := bufio.NewScanner(out)
	sc.Buffer(nil, 1<<26)
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue // a line that is no event, as a build failure prints
		}
		if e.Test == "" || (e.Action != "pass" && e.Action != "fail" && e.Action != "skip") {
			continue
		}
		seen[strings.TrimPrefix(e.Package, module)+"\t"+e.Test] = true
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// The tests are frozen and pass; a run that fails, timed out or was
	// killed may have seen only some subtests, so nothing is written.
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("go test failed, so the list it saw may be partial; nothing written: %w", err)
	}
	if len(seen) == 0 {
		return fmt.Errorf("go test named no test")
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return os.WriteFile(filepath.Join("testdata", "gotest-names.txt"), []byte(strings.Join(names, "\n")+"\n"), 0o644) //nolint:gosec // G306: a committed list
}
