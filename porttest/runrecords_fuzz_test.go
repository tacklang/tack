package porttest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// runRecordSeeds is the directory of FuzzRunRecordReaders' seeds beside
// corpus/runs' archives and invalid.txtar's cases: the reviews' probes
// of the two readers, each a run archive.
const runRecordSeeds = "testdata/run-record-seeds"

// dumpArchive returns data, a run archive, as this package's reader reads
// it, in the form the port's corpus --parse-runs writes (see its
// tools/corpus/dump.tck): archive ok or archive error, then each case,
// its files, and its record's fields or the class of its problem.
func dumpArchive(data []byte) string {
	cases, err := parseRuns(data)
	if err != nil {
		return "archive error\n"
	}
	var b strings.Builder
	b.WriteString("archive ok\n")
	q := strconv.Quote
	for _, c := range cases {
		fmt.Fprintf(&b, "case %s\n", q(c.name))
		for _, p := range c.order {
			fmt.Fprintf(&b, "file %s %d\n", q(p), len(c.files[p]))
		}
		r, err := parseRecord(c, len(c.files) > 0)
		var re *recordError
		if errors.As(err, &re) {
			fmt.Fprintf(&b, "error %s\n", re.class)
			continue
		}
		if err != nil {
			fmt.Fprintf(&b, "error %v\n", err)
			continue
		}
		fmt.Fprintf(&b, "record\nreplaces %d\n", len(r.replaces))
		fmt.Fprintf(&b, "flags selfcheck=%t port-only=%t partial=%t cold=%t strict=%t strict-libs=%t cwd-empty=%t stdin=%t stdout=%t stderr=%t\n",
			r.selfcheck, r.portOnly, r.partial, r.cold, r.strict, r.strictLibs, r.emptyCwd, r.hasStdin, r.hasStdout, r.hasStderr)
		for _, kv := range [][2]string{{"dir", r.dir}, {"target", r.target}, {"stdout-file", r.stdoutF}, {"stderr-file", r.stderrF}, {"must-fail", r.mustFail}} {
			fmt.Fprintf(&b, "%s %s\n", kv[0], q(kv[1]))
		}
		for _, list := range []struct {
			key  string
			vals []string
		}{{"opt", r.opts}, {"cflags", r.cflags}, {"invoke", r.invoke}, {"arg", r.args}, {"env", r.env}} {
			for _, v := range list.vals {
				fmt.Fprintf(&b, "%s %s\n", list.key, q(v))
			}
		}
		fmt.Fprintf(&b, "stdin %s\nnofile %d\n", q(string(r.stdin)), r.nofile)
		fmt.Fprintf(&b, "stream %s %s %s %s\n", q(r.streams["stdout"]), q(r.streams["stderr"]), q(r.streams["fd3"]), q(r.streams["fd4"]))
		fmt.Fprintf(&b, "end exit=%d signal=%d running=%s ends=%d\n", r.code, r.signo, q(r.runText), r.ends)
		for _, kv := range [][2]string{{"stdout", string(r.stdout)}, {"stderr", string(r.stderr)}, {"fd3", string(r.fds["fd3"])}, {"fd4", string(r.fds["fd4"])}} {
			fmt.Fprintf(&b, "%s %s\n", kv[0], q(kv[1]))
		}
		for _, p := range r.wantOrder {
			fmt.Fprintf(&b, "want %s %s\n", q(p), q(string(r.want[p])))
		}
		for _, l := range r.lacks {
			fmt.Fprintf(&b, "lacks %s %s\n", l.stream, q(string(l.bytes)))
		}
		for _, c := range r.counts {
			fmt.Fprintf(&b, "count %s %d %s\n", c.stream, c.n, q(string(c.bytes)))
		}
		for _, p := range r.linkOrder {
			fmt.Fprintf(&b, "link %s %s\n", q(p), q(r.links[p]))
		}
	}
	return b.String()
}

// runRecordReader is the port's corpus runner, built once a process, for
// its --parse-runs.
var runRecordReader = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp(env.tmp, "reader")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "corpus")
	cmd := exec.Command(env.portBin, "--out", bin, "./tools/corpus") //nolint:gosec // G204: the port
	cmd.Dir = env.home
	cmd.Env = compilerEnv(env.portCache)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building tools/corpus with the port: %v\n%s", err, out)
	}
	return bin, nil
})

// FuzzRunRecordReaders feeds each input, a run archive's bytes, to both
// readers of run records, the port's corpus --parse-runs and this
// package's (dumpArchive), and requires the same verdict, byte for byte:
// the same archive, cases and files, and each record read to the same
// fields, or rejected with the same class. It is seeded with
// corpus/runs' archives, with each case of invalid.txtar as an archive of
// its own, and with runRecordSeeds; the fuzz engine adds what it finds
// to testdata/fuzz/FuzzRunRecordReaders, which are seeds from then on.
// It runs as FuzzLexDiff does, with TACK_PORT_TEST=1 and -count, and
// builds the port's tools/corpus once a process.
func FuzzRunRecordReaders(f *testing.F) {
	ready(f)
	archives, err := readRunArchives(env.home)
	if err != nil {
		f.Fatal(err)
	}
	for _, name := range runRecordArchives {
		data, err := os.ReadFile(filepath.Join(env.home, portRunsDir, name)) //nolint:gosec // G304: the corpus
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, c := range archives[runInvalidArchive] {
		var b bytes.Buffer
		b.WriteString("one case of invalid.txtar\n")
		for _, p := range c.order {
			if p != "class" {
				fmt.Fprintf(&b, "-- %s/%s --\n%s", c.name, p, c.files[p])
			}
		}
		fmt.Fprintf(&b, "-- %s/record --\n%s", c.name, c.record)
		f.Add(b.Bytes())
	}
	seeds, err := os.ReadDir(runRecordSeeds)
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range seeds {
		data, err := os.ReadFile(filepath.Join(runRecordSeeds, e.Name())) //nolint:gosec // G304: this module's seeds
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	reader, err := runRecordReader()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		skipUnicodeDrift(t, data)
		file := filepath.Join(t.TempDir(), "runs.txtar")
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(reader, "--parse-runs", file) //nolint:gosec // G204: the port's runner
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			t.Fatalf("corpus --parse-runs: %v\n%s", err, errOut.Bytes())
		}
		want := dumpArchive(data)
		if got := out.String(); got != want {
			t.Errorf("the readers disagree on %q:\nport %s\nGo   %s", firstK(data), firstDiffLine(got, want), firstDiffLine(want, got))
		}
	})
}

// firstDiffLine returns the first line of a that differs from b's line
// at the same place, with its number.
func firstDiffLine(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i, l := range al {
		if i >= len(bl) || bl[i] != l {
			return fmt.Sprintf("line %d: %q", i+1, l)
		}
	}
	return "(no line differs)"
}
