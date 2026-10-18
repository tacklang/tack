package invalid

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Path is where the cases live, relative to a top-level package directory
// such as lexer/ or test/, which is the working directory of that package's
// tests.
const Path = "../examples/invalid.txtar"

// WarningsPath is where the cases of programs that compile with warnings
// live, in the format of Path's, each case's expected output the warnings
// rendered.
const WarningsPath = "../examples/warnings.txtar"

// File-name suffixes that make up one case in the archive.
const (
	sourceSuffix    = "/main.tck"
	expectedSuffix  = "/expected"
	noNewlineSuffix = "/no-final-newline"
)

// Header markers: a line that starts with headerStart and ends with
// headerEnd introduces a file.
const (
	headerStart = "-- "
	headerEnd   = " --"
)

// Case is one invalid example program and what the compiler reports for it.
type Case struct {
	// Name identifies the case. It is the directory part of the case's file
	// names in the archive, and the subtest name in the golden harness.
	Name string
	// Source is the program, byte for byte.
	Source []byte
	// Expected is the compiler's output for Source, as the golden harness
	// renders it.
	Expected []byte
}

// file is one txtar file: its header name, the line its header is on, and
// its content.
type file struct {
	name string
	data []byte
	line int
}

// Load reads the archive at path and parses it. It fails if the archive
// holds no cases, so a caller that ranges over the result cannot pass
// vacuously.
func Load(path string) ([]Case, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the archive by design
	if err != nil {
		return nil, fmt.Errorf("reading invalid examples: %w", err)
	}
	_, cases, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: no cases", path)
	}
	return cases, nil
}

// Parse splits an archive into its leading comment and its cases. It
// returns an error naming the offending line for any structure the package
// doc rules out.
func Parse(data []byte) (comment []byte, cases []Case, err error) {
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, nil, fmt.Errorf("archive does not end with a newline")
	}
	comment, files, err := split(data)
	if err != nil {
		return nil, nil, err
	}
	for len(files) > 0 {
		var c Case
		c, files, err = takeCase(files)
		if err != nil {
			return nil, nil, err
		}
		if n := len(cases); n > 0 && cases[n-1].Name >= c.Name {
			return nil, nil, fmt.Errorf(
				"case %q comes after %q: cases must be in strictly increasing order",
				c.Name,
				cases[n-1].Name,
			)
		}
		cases = append(cases, c)
	}
	return comment, cases, nil
}

// split cuts data into the comment before the first header and the files
// after it. data is known to be empty or to end with a newline.
func split(data []byte) (comment []byte, files []file, err error) {
	var cur *file
	for i, line := range bytes.SplitAfter(data, []byte("\n")) {
		if len(line) == 0 {
			break // the empty tail SplitAfter leaves after the final newline
		}
		text := string(line[:len(line)-1])
		if !isHeader(text) {
			if cur == nil {
				comment = append(comment, line...)
			} else {
				cur.data = append(cur.data, line...)
			}
			continue
		}
		name := ""
		if len(text) > len(headerStart)+len(headerEnd) {
			name = text[len(headerStart) : len(text)-len(headerEnd)]
		}
		if name == "" || strings.TrimSpace(name) != name {
			return nil, nil, fmt.Errorf("line %d: malformed header %q", i+1, text)
		}
		files = append(files, file{name: name, line: i + 1})
		cur = &files[len(files)-1]
	}
	return comment, files, nil
}

// isHeader reports whether line, without its newline, would be read as a
// file header. "-- --" counts, so that it is rejected rather than taken as
// content by one reader and as a header by another.
func isHeader(line string) bool {
	return strings.HasPrefix(line, headerStart) && strings.HasSuffix(line, headerEnd)
}

// takeCase reads the case that starts at files[0] and returns it with the
// files that follow it.
func takeCase(files []file) (Case, []file, error) {
	src := files[0]
	name, ok := strings.CutSuffix(src.name, sourceSuffix)
	if !ok || checkName(name) != nil {
		return Case{}, nil, fmt.Errorf("line %d: want a header -- <case>%s --, got %q",
			src.line, sourceSuffix, src.name)
	}
	if len(files) < 2 || files[1].name != name+expectedSuffix {
		return Case{}, nil, fmt.Errorf("line %d: case %q: want -- %s%s -- right after its program",
			src.line, name, name, expectedSuffix)
	}
	exp := files[1]
	if len(exp.data) == 0 {
		return Case{}, nil, fmt.Errorf(
			"line %d: case %q has an empty expected output",
			exp.line,
			name,
		)
	}
	c := Case{Name: name, Source: src.data, Expected: exp.data}
	rest := files[2:]
	if len(rest) > 0 && rest[0].name == name+noNewlineSuffix {
		if err := stripNewline(&c, rest[0]); err != nil {
			return Case{}, nil, err
		}
		rest = rest[1:]
	}
	return c, rest, nil
}

// stripNewline applies a no-final-newline marker to c.
func stripNewline(c *Case, marker file) error {
	if len(marker.data) != 0 {
		return fmt.Errorf("line %d: case %q: the no-final-newline marker must be empty",
			marker.line, c.Name)
	}
	if len(c.Source) == 0 {
		return fmt.Errorf("line %d: case %q: an empty program has no newline to drop",
			marker.line, c.Name)
	}
	c.Source = c.Source[:len(c.Source)-1]
	if len(c.Source) == 0 || c.Source[len(c.Source)-1] == '\n' {
		return fmt.Errorf("line %d: case %q: the program's last line is empty, so the "+
			"no-final-newline marker would not leave it unterminated", marker.line, c.Name)
	}
	return nil
}

// checkName reports whether name can be a case name: non-empty, without a
// slash, and without spaces, which would make its headers ambiguous.
func checkName(name string) error {
	if name == "" || strings.ContainsAny(name, "/ \t\r\n") {
		return fmt.Errorf("invalid case name %q", name)
	}
	return nil
}

// Format renders comment and cases as an archive that Parse turns back into
// the same comment and cases. It fails, naming the case, for anything the
// format cannot hold exactly.
func Format(comment []byte, cases []Case) ([]byte, error) {
	if err := checkContent("the comment", comment); err != nil {
		return nil, err
	}
	if len(comment) > 0 && comment[len(comment)-1] != '\n' {
		return nil, fmt.Errorf("the comment does not end with a newline")
	}
	var buf bytes.Buffer
	buf.Write(comment)
	for i, c := range cases {
		if err := checkCase(c); err != nil {
			return nil, err
		}
		if i > 0 && cases[i-1].Name >= c.Name {
			return nil, fmt.Errorf(
				"case %q comes after %q: cases must be in strictly increasing order",
				c.Name,
				cases[i-1].Name,
			)
		}
		writeCase(&buf, c)
	}
	return buf.Bytes(), nil
}

// checkCase reports why c cannot be written to an archive, if it cannot.
func checkCase(c Case) error {
	if err := checkName(c.Name); err != nil {
		return err
	}
	if err := checkContent(c.Name+sourceSuffix, c.Source); err != nil {
		return err
	}
	if err := checkContent(c.Name+expectedSuffix, c.Expected); err != nil {
		return err
	}
	if len(c.Expected) == 0 || c.Expected[len(c.Expected)-1] != '\n' {
		return fmt.Errorf(
			"case %q: the expected output must be non-empty and end with a newline",
			c.Name,
		)
	}
	return nil
}

// checkContent fails if data has a line that would read as a header.
func checkContent(what string, data []byte) error {
	for i, line := range strings.Split(string(data), "\n") {
		if isHeader(line) {
			return fmt.Errorf("%s: line %d reads as a txtar header: %q", what, i+1, line)
		}
	}
	return nil
}

// writeCase appends c to buf, adding the no-final-newline marker when the
// program needs it. c has passed checkCase.
func writeCase(buf *bytes.Buffer, c Case) {
	fmt.Fprintf(buf, "%s%s%s%s\n", headerStart, c.Name, sourceSuffix, headerEnd)
	buf.Write(c.Source)
	unterminated := len(c.Source) > 0 && c.Source[len(c.Source)-1] != '\n'
	if unterminated {
		buf.WriteByte('\n')
	}
	fmt.Fprintf(buf, "%s%s%s%s\n", headerStart, c.Name, expectedSuffix, headerEnd)
	buf.Write(c.Expected)
	if unterminated {
		fmt.Fprintf(buf, "%s%s%s%s\n", headerStart, c.Name, noNewlineSuffix, headerEnd)
	}
}
