package porttest

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// runRecordsInventory is the committed list of every leaf test of the
// inventoried Go tests whose runtime assertions are to be run records,
// one per line: the file below this module's root, a tab, the test with
// its subtests, a tab, the class the inventory gave its function, a or b,
// a tab, and its status: covered, partial, todo, not yet converted, then
// a tab and why it waits when the reason is known, or excluded, then a
// tab and why.
const runRecordsInventory = "testdata/runrecords.tsv"

// runRecordsCounts is the committed count of runRecordsInventory's lines
// by status, one line each, the status, a space and the count, which
// TestRunRecordsCoverage requires the inventory to have: each batch of
// conversions changes it on purpose.
const runRecordsCounts = "testdata/runrecords-counts.txt"

// runRecordsNames is the committed list of every test and subtest go
// test runs in the packages whose tests are moving to records, one per
// line, the package's directory, a tab and the name, which go generate
// writes (see testnames) and checkNamesFresh keeps in step.
const runRecordsNames = "testdata/gotest-names.txt"

// recordPackages are the packages whose Go tests are moving to records.
var recordPackages = []string{"codegen/c", "tack", "test", "internal/progen"}

// path2pkg returns the package directory of a test file below this
// module's root.
func path2pkg(file string) string { return filepath.ToSlash(filepath.Dir(file)) }

// inventoryLine is one line of runRecordsInventory.
type inventoryLine struct{ file, test, class, status, why string }

// TestRunRecordsCoverage checks the inventory, runRecordsInventory,
// against the records and the subtest names, runRecordsNames (see
// checkNamesFresh):
//
//   - every line names a leaf test go test runs, a test with no subtest;
//   - every replaces key is a test go test runs and a line of the
//     inventory, and a leaf, unless its record says partial;
//   - every leaf of a function the inventory names is a line of it, so
//     no subtest of an inventoried test is forgotten;
//   - each line's status is what the records make it: covered when
//     records replace it and none says partial, partial when one does,
//     todo or excluded, with why, when none replaces it;
//   - the counts by status are runRecordsCounts', which it also writes
//     on standard error, so a change of coverage is seen and made on
//     purpose.
//
// It reads only files, and needs no port binary, but the port's source
// tree.
func TestRunRecordsCoverage(t *testing.T) {
	ready(t)
	archives, err := readRunArchives(env.home)
	if err != nil {
		t.Fatal(err)
	}
	names := readNames(t)
	listed := readInventory(t)
	replaced, partial := map[string]bool{}, map[string]bool{}
	for archive, cases := range archives {
		if archive == runInvalidArchive {
			continue
		}
		for _, c := range cases {
			r, err := parseRecord(c, len(c.files) > 0)
			if err != nil {
				t.Errorf("%s/%s: %v", archive, c.name, err)
				continue
			}
			if archive == runMustFailArchive && len(r.replaces) > 0 {
				t.Errorf("%s/%s replaces a test, which a record that must fail cannot do", archive, c.name)
				continue
			}
			for _, rep := range r.replaces {
				replaced[rep] = true
				if r.partial {
					partial[rep] = true
				}
				file, test, _ := strings.Cut(rep, " ")
				key := path2pkg(file) + "\t" + test
				if !names[key] {
					t.Errorf("%s/%s replaces %s, which go test does not run (see %s)", archive, c.name, rep, runRecordsNames)
				} else if !isLeaf(names, key) && !r.partial {
					t.Errorf("%s/%s replaces %s, which has subtests: name each leaf, or say partial", archive, c.name, rep)
				}
				if _, ok := listed[rep]; !ok {
					t.Errorf("%s/%s replaces %s, which %s does not list", archive, c.name, rep, runRecordsInventory)
				}
			}
		}
	}
	funcs := map[string]string{}
	for _, l := range listed {
		key := path2pkg(l.file) + "\t" + l.test
		if !names[key] {
			t.Errorf("%s lists %s %s, which go test does not run", runRecordsInventory, l.file, l.test)
		} else if !isLeaf(names, key) {
			t.Errorf("%s lists %s %s, which has subtests: list its leaves instead", runRecordsInventory, l.file, l.test)
		}
		top, _, _ := strings.Cut(l.test, "/")
		funcs[path2pkg(l.file)+"\t"+top] = l.file
	}
	for f, file := range funcs {
		pkg, top, _ := strings.Cut(f, "\t")
		for n := range names {
			p, test, _ := strings.Cut(n, "\t")
			if p != pkg || (test != top && !strings.HasPrefix(test, top+"/")) || !isLeaf(names, n) {
				continue
			}
			if _, ok := listed[file+" "+test]; !ok {
				t.Errorf("%s lists %s %s but not its leaf %s: list it as covered, partial, todo or excluded", runRecordsInventory, file, top, test)
			}
		}
	}
	counts := map[string]int{}
	for key, l := range listed {
		want := "todo"
		switch {
		case partial[key]:
			want = "partial"
		case replaced[key]:
			want = "covered"
		}
		if !(l.status == want || want == "todo" && l.status == "excluded") {
			t.Errorf("%s lists %s as %s; its records make it %s", runRecordsInventory, key, l.status, want)
		}
		counts[l.status]++
	}
	summary := fmt.Sprintf("covered %d\npartial %d\ntodo %d\nexcluded %d\n",
		counts["covered"], counts["partial"], counts["todo"], counts["excluded"])
	fmt.Fprintf(os.Stderr, "run records: %d functions, leaves %s", len(funcs), strings.ReplaceAll(strings.TrimSuffix(summary, "\n"), "\n", ", "))
	fmt.Fprintln(os.Stderr)
	want, err := os.ReadFile(runRecordsCounts)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != summary {
		t.Errorf("the inventory's counts by status are\n%s, and %s holds\n%s: change it with the conversion that changes them", summary, runRecordsCounts, want)
	}
}

// isLeaf reports whether the test key, "<pkg>\t<name>", has no subtest
// in names.
func isLeaf(names map[string]bool, key string) bool {
	for n := range names {
		if strings.HasPrefix(n, key+"/") {
			return false
		}
	}
	return true
}

// readInventory reads runRecordsInventory, by "<file> <test>", and checks
// each line's shape and that the test's top-level function is in its
// file.
func readInventory(t *testing.T) map[string]inventoryLine {
	t.Helper()
	data, err := os.ReadFile(runRecordsInventory)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]inventoryLine{}
	funcs := map[string][]string{}
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		f := strings.Split(line, "\t")
		ok := len(f) >= 4 && (f[2] == "a" || f[2] == "b")
		if ok {
			switch f[3] {
			case "covered", "partial":
				ok = len(f) == 4
			case "todo":
				ok = len(f) == 4 || len(f) == 5 && f[4] != ""
			case "excluded":
				ok = len(f) == 5 && f[4] != ""
			default:
				ok = false
			}
		}
		if !ok {
			t.Fatalf("%s:%d: want file, test, class a or b, and covered, partial, todo and maybe why, or excluded and why, tab separated", runRecordsInventory, i+1)
		}
		l := inventoryLine{file: f[0], test: f[1], class: f[2], status: f[3]}
		if len(f) == 5 {
			l.why = f[4]
		}
		key := f[0] + " " + f[1]
		if _, dup := listed[key]; dup {
			t.Errorf("%s:%d: %s is listed twice", runRecordsInventory, i+1, key)
		}
		listed[key] = l
		names, ok := funcs[f[0]]
		if !ok {
			names = testFuncs(t, filepath.Join(env.root, f[0]))
			funcs[f[0]] = names
		}
		top, _, _ := strings.Cut(f[1], "/")
		if !slices.Contains(names, top) {
			t.Errorf("%s:%d: %s has no function %s", runRecordsInventory, i+1, f[0], top)
		}
	}
	return listed
}

// readNames reads runRecordsNames into a set, after checkNamesFresh.
func readNames(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(runRecordsNames)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for line := range strings.Lines(string(data)) {
		names[strings.TrimSuffix(line, "\n")] = true
	}
	checkNamesFresh(t, names)
	return names
}

// checkNamesFresh checks that the top-level tests runRecordsNames holds,
// names, are exactly the Test and Fuzz functions of recordPackages' test
// files that this platform builds: a test added, removed or renamed since
// go generate wrote the list fails here, so the list cannot go stale
// unnoticed. Subtests are checked only through their test, which go
// generate must then be run again for.
func checkNamesFresh(t *testing.T, names map[string]bool) {
	t.Helper()
	want := map[string]bool{}
	for _, pkg := range recordPackages {
		dir := filepath.Join(env.root, filepath.FromSlash(pkg))
		files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		mustDo(t, err)
		for _, f := range files {
			if ok, err := build.Default.MatchFile(dir, filepath.Base(f)); err != nil || !ok {
				continue
			}
			for _, fn := range testFuncs(t, f) {
				if (strings.HasPrefix(fn, "Test") || strings.HasPrefix(fn, "Fuzz")) && fn != "TestMain" {
					want[pkg+"\t"+fn] = true
				}
			}
		}
	}
	have := map[string]bool{}
	for n := range names {
		if pkg, test, ok := strings.Cut(n, "\t"); ok && !strings.Contains(test, "/") {
			have[pkg+"\t"+test] = true
		}
	}
	for n := range want {
		if !have[n] {
			t.Errorf("%s lacks %s: run go generate ./porttest", runRecordsNames, n)
		}
	}
	for n := range have {
		if !want[n] {
			t.Errorf("%s holds %s, which is no test now: run go generate ./porttest", runRecordsNames, n)
		}
	}
}

// testFuncs returns the names of the top-level functions of the Go file
// path.
func testFuncs(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
			names = append(names, fd.Name.Name)
		}
	}
	return names
}

// TestRungenLines runs porttest/rungen into a directory of its own and
// checks each archive it writes against the committed one of corpus/runs
// of the same name, prototype.txtar and batch 1's tack-*.txtar: every
// case it writes is a case of the committed archive with the same
// files, and every line of its record is a line of the committed one,
// verbatim, so a value typed into a record by hand, an stdin, arg, env,
// opt or stream line among them, that is not what the Go test holds fails
// here; what the committed record holds besides is what --update wrote,
// the expectations the Go tests do not pin. It needs the port's source
// tree, but no compiler but Go's toolchain.
func TestRungenLines(t *testing.T) {
	ready(t)
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "go", "run", "./porttest/rungen", dir)
	cmd.Dir = env.root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run ./porttest/rungen: %v\n%s", err, out)
	}
	read := func(p string) map[string]recordCase {
		data, err := os.ReadFile(p) //nolint:gosec // G304: an archive of the corpus or rungen's
		mustDo(t, err)
		cases, err := parseRuns(data)
		mustDo(t, err)
		out := map[string]recordCase{}
		for _, c := range cases {
			out[c.name] = c
		}
		return out
	}
	written, err := os.ReadDir(dir)
	mustDo(t, err)
	if len(written) == 0 {
		t.Fatal("rungen writes no archive")
	}
	for _, w := range written {
		t.Run(w.Name(), func(t *testing.T) {
			generated := read(filepath.Join(dir, w.Name()))
			committed := read(filepath.Join(env.home, portRunsDir, w.Name()))
			compareRungen(t, generated, committed)
		})
	}
}

// compareRungen checks the cases rungen wrote, generated, against the
// committed archive's, as TestRungenLines describes.
func compareRungen(t *testing.T, generated, committed map[string]recordCase) {
	t.Helper()
	for name, g := range generated {
		c, ok := committed[name]
		if !ok {
			t.Errorf("rungen writes the record %s, which the committed archive lacks", name)
			continue
		}
		for p, data := range g.files {
			if !bytes.Equal(c.files[p], data) {
				t.Errorf("%s: the committed file %s is not what rungen writes", name, p)
			}
		}
		if len(c.files) != len(g.files) {
			t.Errorf("%s: the committed case has %d files, rungen writes %d", name, len(c.files), len(g.files))
		}
		have := map[string]int{}
		for _, l := range strings.SplitAfter(string(c.record), "\n") {
			have[l]++
		}
		for _, l := range strings.SplitAfter(string(g.record), "\n") {
			if l == "" {
				continue
			}
			if have[l] == 0 {
				t.Errorf("%s: rungen writes the line %s, which the committed record lacks", name, strconv.Quote(strings.TrimSuffix(l, "\n")))
				continue
			}
			have[l]--
		}
	}
	for name := range committed {
		if _, ok := generated[name]; !ok {
			t.Errorf("the committed archive holds %s, which rungen does not write", name)
		}
	}
}
