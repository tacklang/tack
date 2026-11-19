package porttest

//go:generate go run ./testnames

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The reader of the port's run records (see the port's
// tools/corpus/runs.tck for the format), written apart from the port's,
// so that a record the two read differently fails one side; corpus/runs'
// invalid.txtar holds the records both must reject, each with its class.
// runrecords_run_test.go runs the records, and runrecords_cover_test.go
// checks what they cover.

// runRecord is one case of a run archive, read by parseRecord: the
// case's files, none when it is built from dir, and what the record says.
type runRecord struct {
	name      string
	files     map[string][]byte
	replaces  []string // "<file> <test>"
	partial   bool
	selfcheck bool
	cold      bool
	dir       string
	target    string
	opts      []string
	cflags    []string
	strict    bool
	invoke    []string
	emptyCwd  bool
	args      []string
	env       []string
	stdin     []byte
	hasStdin  bool
	nofile    int
	streams   map[string]string // stdout, stderr, fd3, fd4: how each is
	code      int
	signo     int
	running   time.Duration
	runText   string
	mustFail  string // the failure class a must-fail record must fail with
	ends      int
	stdout    []byte
	hasStdout bool
	stdoutF   string
	stderr    []byte
	hasStderr bool
	stderrF   string
	fds       map[string][]byte // fd3, fd4: what each captured must hold
	want      map[string][]byte
	wantOrder []string
	links     map[string]string // symlink lines: each link's path and its target
	linkOrder []string
}

// runRecordsSuite is the suite of testdata/divergences.txtar whose
// entries are run records': each names its record as <archive>#<name>.
const runRecordsSuite = "run-records"

// runInvalidArchive is the archive of corpus/runs whose cases are
// records every reader must reject, each with the class its class file
// names.
const runInvalidArchive = "invalid.txtar"

// runRecordArchives are the archives corpus/runs must hold, no more and
// no fewer.
var runRecordArchives = []string{runInvalidArchive, runMustFailArchive, "prototype.txtar"}

// runMustFailArchive is the archive of corpus/runs whose records must
// fail, each with the failure class its must-fail line names, and the
// only one whose records have must-fail lines.
const runMustFailArchive = "must-fail.txtar"

// runFailureClasses are the classes of the ways a run can fail, in the
// order both runners check them, the first one found being the run's,
// as runs.tck's failure_classes lists them; must-fail names one.
var runFailureClasses = []string{"build", "build-tmpdir", "outlived", "cwd", "src-changed", "run-root", "tmpdir",
	"exit", "still-running", "did-not-end", "stdout", "stderr", "fd3", "fd4", "files"}

// runStreamWays are the ways a record may make each descriptor.
var runStreamWays = map[string][]string{
	"stdout": {"closed", "broken", "file"},
	"stderr": {"closed", "broken", "file", "merged"},
	"fd3":    {"capture", "broken"},
	"fd4":    {"capture", "broken"},
}

// recordCase is one case of a run archive as parseRuns splits it: its
// name, its files and its record's bytes.
type recordCase struct {
	name   string
	files  map[string][]byte
	order  []string // the files' paths, in archive order
	record []byte
}

// parseRuns reads a run archive: a comment, then cases in increasing
// byte order of their names, each its files, <case>/<path>, and then
// <case>/record, which ends it. It splits the bytes as they are, a
// carriage return kept wherever it is, and takes a line for a header as
// internal/invalid does: one that starts with "-- " and ends with " --",
// "-- --" among them, whose name, what is between, must be neither empty
// nor start or end with a space.
func parseRuns(data []byte) ([]recordCase, error) {
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		return nil, errors.New("archive does not end with a newline")
	}
	type section struct {
		name string
		line int
		body []byte
	}
	var secs []section
	for i, l := range bytes.SplitAfter(data, []byte("\n")) {
		t := bytes.TrimSuffix(l, []byte("\n"))
		if bytes.HasPrefix(t, []byte("-- ")) && bytes.HasSuffix(t, []byte(" --")) {
			name := ""
			if len(t) > 6 {
				name = string(t[3 : len(t)-3])
			}
			if name == "" || !isTrimmed(name) {
				return nil, fmt.Errorf("line %d: malformed header %q", i+1, t)
			}
			secs = append(secs, section{name: name, line: i + 1})
			continue
		}
		if len(secs) > 0 {
			secs[len(secs)-1].body = append(secs[len(secs)-1].body, l...)
		}
	}
	var out []recordCase
	var cur *recordCase
	for _, s := range secs {
		name, rel, ok := strings.Cut(s.name, "/")
		if !ok || name == "" {
			return nil, fmt.Errorf("line %d: want a header -- <case>/<path> --, got %q", s.line, s.name)
		}
		if cur != nil && cur.name != name {
			return nil, fmt.Errorf("line %d: case %q ends without its record", s.line, cur.name)
		}
		if cur == nil {
			if strings.ContainsAny(name, " \t\r\n") {
				return nil, fmt.Errorf("line %d: case %q: invalid case name", s.line, name)
			}
			if len(out) > 0 && out[len(out)-1].name >= name {
				return nil, fmt.Errorf("line %d: case %q: out of order", s.line, name)
			}
			cur = &recordCase{name: name, files: map[string][]byte{}}
		}
		if rel == "record" {
			cur.record = s.body
			out = append(out, *cur)
			cur = nil
			continue
		}
		if _, dup := cur.files[rel]; dup || !cleanPath(rel) {
			return nil, fmt.Errorf("line %d: case %q: bad or repeated file %q", s.line, name, rel)
		}
		cur.files[rel] = s.body
		cur.order = append(cur.order, rel)
	}
	if cur != nil {
		return nil, fmt.Errorf("case %q ends without its record", cur.name)
	}
	if len(out) == 0 {
		return nil, errors.New("it holds no cases")
	}
	return out, nil
}

// isTrimmed reports whether s neither starts nor ends with a space, as
// Go's strings.TrimSpace, which internal/invalid's headers are held to,
// sees one.
func isTrimmed(s string) bool { return strings.TrimSpace(s) == s }

// recordError is why a record cannot be read: its class, as runs.tck
// names them, and the message.
type recordError struct{ class, msg string }

// Error returns the error as the port's runner writes it: [class] msg.
func (e *recordError) Error() string { return "[" + e.class + "] " + e.msg }

// bad returns a recordError of class with the message made of format.
func bad(class, format string, args ...any) error {
	return &recordError{class: class, msg: fmt.Sprintf(format, args...)}
}

// hexVal returns the value of the hexadecimal digit c, either case, or
// -1.
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// quotedPrefix reads the quoted operand that starts s, byte for byte as
// runs.tck's unquote reads one: a double quote, then bytes as they are,
// any byte at all but a double quote, a backslash and a newline, and the
// escapes \a \b \f \n \r \t \v \\ \", \x and two hexadecimal digits, a
// byte, and \u and four or \U and eight, a rune that is no surrogate and
// at most U+10FFFF, encoded as UTF-8; then a double quote. It returns the
// bytes and how much of s the literal takes, or false. Go's strconv is no
// help here: it decodes a byte that is no UTF-8 to U+FFFD.
func quotedPrefix(s string) ([]byte, int, bool) {
	if !strings.HasPrefix(s, `"`) {
		return nil, 0, false
	}
	var out []byte
	for i := 1; i < len(s); {
		c := s[i]
		switch c {
		case '"':
			return out, i + 1, true
		case '\n':
			return nil, 0, false
		case '\\':
		default:
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return nil, 0, false
		}
		e := s[i+1]
		if l := strings.IndexByte("abfnrtv\\\"", e); l >= 0 {
			out = append(out, "\a\b\f\n\r\t\v\\\""[l])
			i += 2
			continue
		}
		digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
		if digits == 0 || i+2+digits > len(s) {
			return nil, 0, false
		}
		v := 0
		for _, d := range []byte(s[i+2 : i+2+digits]) {
			h := hexVal(d)
			if h < 0 {
				return nil, 0, false
			}
			v = v*16 + h
		}
		if e == 'x' {
			out = append(out, byte(v))
		} else {
			if v > 0x10FFFF || v >= 0xD800 && v <= 0xDFFF {
				return nil, 0, false
			}
			out = utf8.AppendRune(out, rune(v))
		}
		i += 2 + digits
	}
	return nil, 0, false
}

// quoted reads s, all of it one quoted operand (see quotedPrefix).
func quoted(s string) ([]byte, bool) {
	b, n, ok := quotedPrefix(s)
	return b, ok && n == len(s)
}

// isFlag reports whether f is a flag cflags takes: a - and then letters,
// digits and _ = , . + / : alone.
func isFlag(f string) bool {
	if len(f) < 2 || f[0] != '-' {
		return false
	}
	for _, c := range []byte(f) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_=,.+/:", c) >= 0) {
			return false
		}
	}
	return true
}

// reservedFlag reports whether f is a flag cflags may not give, as
// runs.tck's is_reserved_flag decides: -O and more, the level, which opt
// alone sets, and -o, -M and its kin, -save-temps, --output and a
// linker's ,-o, which change where clang writes.
func reservedFlag(f string) bool {
	for _, p := range []string{"-O", "-o", "-M", "-save-temps", "--output"} {
		if strings.HasPrefix(f, p) {
			return true
		}
	}
	return strings.Contains(f, ",-o")
}

// runSeconds reads s as still-running takes it, as runs.tck's is_seconds
// does: one or two digits, then, with a dot, one to three digits, from
// 0.1 to 10 seconds.
func runSeconds(s string) (time.Duration, bool) {
	whole, frac, ok := strings.Cut(s, ".")
	if whole == "" || len(whole) > 2 || ok && (frac == "" || len(frac) > 3) || !allDigits(whole) || !allDigits(frac) {
		return 0, false
	}
	w, _ := strconv.Atoi(whole)
	f, _ := strconv.Atoi((frac + "000")[:3])
	ms := w*1000 + f
	if ms < 100 || ms > 10000 {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// allDigits reports whether s is ASCII digits alone, or empty.
func allDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// count reads s as runs.tck's is_count does: one to nine digits alone.
func count(s string) (int, bool) {
	if s == "" || len(s) > 9 || !allDigits(s) {
		return 0, false
	}
	n, _ := strconv.Atoi(s)
	return n, true
}

// cleanPath reports whether p is a path every record takes for a file
// below a directory of its own, as runs.tck's is_clean_path does: not
// empty, relative, its parts none empty, none . or .., and no 0 byte.
func cleanPath(p string) bool {
	if p == "" || strings.IndexByte(p, 0) >= 0 {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// envName reports whether s is a name env takes: a letter or _, then
// letters, digits and _.
func envName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range []byte(s) {
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// replacesKey reports whether s is what replaces takes: a clean path of
// a _test.go file, a space, and a Test or Fuzz name with no space.
func replacesKey(s string) bool {
	file, test, ok := strings.Cut(s, " ")
	return ok && strings.Count(s, " ") == 1 && cleanPath(file) && strings.HasSuffix(file, "_test.go") &&
		(strings.HasPrefix(test, "Test") || strings.HasPrefix(test, "Fuzz"))
}

// parseRecord reads c's record into a runRecord, rejecting what the
// port's runner rejects with the same class: hasFiles says whether the
// case has files of its own.
func parseRecord(c recordCase, hasFiles bool) (runRecord, error) {
	r := runRecord{name: c.name, files: c.files, want: map[string][]byte{}, streams: map[string]string{},
		fds: map[string][]byte{}, strict: true, links: map[string]string{}}
	openFile := ""
	once := map[string]bool{}
	lines := bytes.SplitAfter(c.record, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	for k, l := range lines {
		line := string(bytes.TrimSuffix(l, []byte("\n")))
		key, rest, ok := strings.Cut(line, " ")
		if !ok || key == "" {
			return r, fmt.Errorf("line %d: %w", k+1, bad("syntax", "want a keyword, a space and its operands"))
		}
		if rest == "" {
			return r, fmt.Errorf("line %d: %w", k+1, bad("empty", "an operand is empty"))
		}
		if key != "file+" {
			openFile = ""
		}
		if err := recordLine(&r, key, rest, once, &openFile); err != nil {
			return r, fmt.Errorf("line %d: %w", k+1, err)
		}
	}
	return r, wholeRecord(r, hasFiles)
}

// recordLine reads one line of a record, its keyword key and its
// operands rest, into r; once notes the keywords given once at most, and
// openFile the file the last file line named, which a file+ line goes on.
func recordLine(r *runRecord, key, rest string, once map[string]bool, openFile *string) error {
	single := func(k string) error {
		if once[k] {
			return bad("repeated", "%s is given once at most", k)
		}
		once[k] = true
		return nil
	}
	operand := func() ([]byte, error) {
		v, ok := quoted(rest)
		if !ok {
			return nil, bad("quote", "want one quoted operand, got %q", rest)
		}
		return v, nil
	}
	switch key {
	case "note":
	case "replaces":
		if !replacesKey(rest) {
			return bad("replaces", "replaces takes a clean path of a _test.go file, a space and a Test or Fuzz name")
		}
		r.replaces = append(r.replaces, rest)
	case "partial":
		r.partial = true
	case "selfcheck":
		r.selfcheck = true
	case "no-strict-c", "cold-cache":
		if err := single(key); err != nil {
			return err
		}
		if key == "cold-cache" {
			r.cold = true
		} else {
			r.strict = false
		}
	case "dir", "target", "stdout-file", "stderr-file":
		if err := single(key); err != nil {
			return bad("repeated", "a keyword that is given once at most is repeated")
		}
		if !cleanPath(rest) || key == "dir" && !strings.HasPrefix(rest, "examples/") {
			return bad("path", "want a clean relative path, below examples/ for dir")
		}
		switch key {
		case "dir":
			r.dir = rest
		case "target":
			r.target = rest
		case "stdout-file":
			r.stdoutF = rest
		default:
			r.stderrF = rest
		}
	case "must-fail":
		if !slices.Contains(runFailureClasses, rest) {
			return bad("failure", "must-fail takes one of the failure classes")
		}
		if r.mustFail != "" {
			return bad("repeated", "must-fail is given once at most")
		}
		r.mustFail = rest
	case "cwd":
		if rest != "empty" {
			return bad("syntax", "want cwd empty")
		}
		if err := single(key); err != nil {
			return err
		}
		r.emptyCwd = true
	case "opt", "cflags":
		if err := single(key); err != nil {
			return bad("repeated", "opt and cflags are given once at most")
		}
		for _, w := range strings.Split(rest, " ") {
			if key == "opt" {
				if !slices.Contains([]string{"0", "1", "2", "3", "s", "z"}, w) || slices.Contains(r.opts, w) {
					return bad("opt", "each level is one of 0 1 2 3 s z, given once")
				}
				r.opts = append(r.opts, w)
			} else {
				if !isFlag(w) || reservedFlag(w) {
					return bad("cflags", "each flag is a - and then letters, digits and _ = , . + / : alone, and none sets the level or where clang writes")
				}
				r.cflags = append(r.cflags, w)
			}
		}
	case "stream":
		fd, how, ok := strings.Cut(rest, " ")
		if !ok || fd == "" {
			return bad("stream", "stream takes a descriptor and how it is")
		}
		if !slices.Contains(runStreamWays[fd], how) {
			return bad("stream", "no such descriptor, or a way it cannot be")
		}
		if r.streams[fd] != "" {
			return bad("repeated", "a descriptor's way is given once at most")
		}
		r.streams[fd] = how
	case "arg", "env", "invoke", "stdin", "stdout", "stderr", "fd3", "fd4":
		b, err := operand()
		if err != nil {
			return err
		}
		switch key {
		case "arg":
			r.args = append(r.args, string(b))
		case "env":
			name, _, ok := strings.Cut(string(b), "=")
			if !ok || !envName(name) || slices.Contains([]string{"PATH", "TMPDIR", "TACK_HOME", "TACK_CACHE"}, name) {
				return bad("env", "env takes NAME=value, NAME a letter or _ and then letters, digits and _, and none the runner sets")
			}
			r.env = append(r.env, string(b))
		case "invoke":
			r.invoke = append(r.invoke, string(b))
		case "stdin":
			r.stdin, r.hasStdin = append(r.stdin, b...), true
		case "stdout":
			r.stdout, r.hasStdout = append(r.stdout, b...), true
		case "stderr":
			r.stderr, r.hasStderr = append(r.stderr, b...), true
		default:
			r.fds[key] = append(r.fds[key], b...)
		}
	case "limit":
		n, ok := strings.CutPrefix(rest, "nofile ")
		if !ok {
			return bad("syntax", "want limit nofile <n>")
		}
		if err := single(key); err != nil {
			return err
		}
		v, ok := count(n)
		if !ok || v <= 0 || v > 65536 {
			return bad("number", "the limit is a number from 1 to 65536")
		}
		r.nofile = v
	case "still-running":
		d, ok := runSeconds(rest)
		if !ok {
			return bad("number", "still-running takes seconds from 0.1 to 10, with three digits after the dot at most")
		}
		if r.running == 0 {
			r.running, r.runText = d, rest
		}
		r.ends++
	case "exit", "signal":
		n, ok := count(rest)
		if !ok || n > 255 || key == "signal" && n == 0 {
			return bad("number", "want an exit status from 0 to 255, or a signal from 1 to 255")
		}
		if key == "exit" {
			r.code = n
		} else {
			r.signo = n
		}
		r.ends++
	case "file":
		pathB, n, ok1 := quotedPrefix(rest)
		after, spaced := "", false
		if ok1 {
			after, spaced = strings.CutPrefix(rest[n:], " ")
		}
		body, ok2 := quoted(after)
		if !ok1 || !spaced || !ok2 {
			return bad("quote", "file takes a quoted path and quoted bytes")
		}
		path := string(pathB)
		if !cleanPath(path) {
			return bad("path", "the file's path is no clean relative path")
		}
		_, dupFile := r.want[path]
		_, dupLink := r.links[path]
		if dupFile || dupLink {
			return bad("repeated", "the file is named twice")
		}
		r.want[path] = body
		r.wantOrder = append(r.wantOrder, path)
		*openFile = path
	case "file+":
		b, err := operand()
		if err != nil {
			return err
		}
		if *openFile == "" {
			return bad("syntax", "file+ goes on the file line before it, and follows none")
		}
		r.want[*openFile] = append(r.want[*openFile], b...)
	case "symlink":
		pathB, n, ok1 := quotedPrefix(rest)
		after, spaced := "", false
		if ok1 {
			after, spaced = strings.CutPrefix(rest[n:], " ")
		}
		target, ok2 := quoted(after)
		if !ok1 || !spaced || !ok2 {
			return bad("quote", "symlink takes a quoted path and a quoted target")
		}
		path := string(pathB)
		if !cleanPath(path) || len(target) == 0 || bytes.IndexByte(target, 0) >= 0 {
			return bad("path", "the link's path is no clean relative path, or its target is empty or holds a 0 byte")
		}
		_, dupFile := r.want[path]
		_, dupLink := r.links[path]
		if dupFile || dupLink {
			return bad("repeated", "the file is named twice")
		}
		r.links[path] = string(target)
		r.linkOrder = append(r.linkOrder, path)
	default:
		return bad("unknown", "unknown keyword %q", key)
	}
	return nil
}

// wholeRecord rejects what r, a whole record, lacks or holds that cannot
// go together, in runs.tck's record_whole's order.
func wholeRecord(r runRecord, hasFiles bool) error {
	merged := r.streams["stderr"] == "merged"
	switch {
	case len(r.replaces) == 0 && !r.selfcheck:
		return bad("missing", "it names no Go test it replaces, and is no selfcheck")
	case r.ends == 0:
		return bad("missing", "it has no exit, signal or still-running line")
	case !hasFiles && r.dir == "":
		return bad("missing", "it has no files of its own and no dir line")
	case r.ends > 1:
		return bad("repeated", "it has more than one exit, signal or still-running line")
	case len(r.replaces) > 0 && r.selfcheck:
		return bad("conflict", "a selfcheck replaces no Go test")
	case hasFiles && r.dir != "":
		return bad("conflict", "a case has files of its own or a dir line, not both")
	case r.hasStdout && r.stdoutF != "" || r.hasStderr && r.stderrF != "":
		return bad("conflict", "a stream has both quoted bytes and a file")
	case merged && (r.hasStderr || r.stderrF != ""):
		return bad("conflict", "a merged standard error holds nothing of its own")
	case merged && (r.streams["stdout"] == "closed" || r.streams["stdout"] == "broken"):
		return bad("conflict", "standard error cannot be merged into a closed or broken standard output")
	case len(r.fds["fd3"]) > 0 && r.streams["fd3"] != "capture" || len(r.fds["fd4"]) > 0 && r.streams["fd4"] != "capture":
		return bad("conflict", "fd3 and fd4 bytes need the stream captured")
	case r.emptyCwd && len(r.invoke) == 0:
		return bad("conflict", "cwd empty is for a record that invokes the compiler")
	case len(r.invoke) > 0 && (r.target != "" || len(r.opts) > 0 || len(r.cflags) > 0 || !r.strict || r.cold ||
		len(r.args) > 0 || r.hasStdin || r.nofile > 0 || len(r.streams) > 0 || r.signo > 0 || r.running > 0):
		return bad("conflict", "a record that invokes the compiler builds and runs no program")
	case !(r.hasStdout || r.stdoutF != "") || !(r.hasStderr || r.stderrF != "" || merged):
		return bad("missing", "it pins no standard output or no standard error: every record pins both")
	}
	return nil
}

// readRunArchives reads every archive of the port's corpus/runs, split
// into cases, by name, and checks the set of them is runRecordArchives
// and that each holds a case.
func readRunArchives(home string) (map[string][]recordCase, error) {
	dir := filepath.Join(home, portRunsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]recordCase{}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: the port's corpus
		if err != nil {
			return nil, err
		}
		cases, err := parseRuns(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out[e.Name()] = cases
	}
	if !slices.Equal(names, runRecordArchives) {
		return nil, fmt.Errorf("%s holds %v, want %v (runRecordArchives)", dir, names, runRecordArchives)
	}
	for _, name := range names {
		if len(out[name]) == 0 {
			return nil, fmt.Errorf("%s: the archive holds no case", name)
		}
	}
	return out, nil
}

// TestRunRecordsRejects reads every case of corpus/runs/invalid.txtar,
// each a record every reader must reject, and checks this package's
// reader rejects it with the class its class file names, as the port's
// runner must too.
func TestRunRecordsRejects(t *testing.T) {
	ready(t)
	archives, err := readRunArchives(env.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range archives[runInvalidArchive] {
		t.Run(c.name, func(t *testing.T) {
			want := strings.TrimSpace(string(c.files["class"]))
			files := maps.Clone(c.files)
			delete(files, "class")
			c.files = files
			_, err := parseRecord(c, len(files) > 0)
			var re *recordError
			if !errors.As(err, &re) || re.class != want {
				t.Errorf("read the record with %v, want class %s", err, want)
			}
		})
	}
}
