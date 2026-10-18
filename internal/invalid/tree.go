package invalid

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"
)

// TreePath is where the cases of several files live, relative to a
// top-level package directory, as Path is.
const TreePath = "../examples/invalid-packages.txtar"

// WarningsTreePath is where the cases of several files that compile with
// warnings live, in the format of TreePath's (see WarningsPath).
const WarningsTreePath = "../examples/warnings-packages.txtar"

// CPath is where the cases with C files live, in the format of TreePath's
// archive, relative to a top-level package directory, as Path is. Its
// cases are built into binaries, which compiles their C files with clang,
// so the driver's tests check them rather than the golden harness.
const CPath = "../examples/invalid-c.txtar"

// targetFile is the name, within a tree case, of the optional file that
// says which directory to build. It starts with a dot, so the loader never
// takes it for a source file.
const targetFile = ".target"

// TreeCase is one invalid example made of several files, or of directories
// of files, and what the compiler reports for it.
type TreeCase struct {
	// Name identifies the case, as Case.Name does. Every file of the case
	// is under the directory Name.
	Name string
	// Files are the case's files, in archive order, each with its path
	// relative to the directory Name.
	Files []TreeFile
	// Expected is the compiler's output, as the golden harness renders it.
	Expected []byte
}

// TreeFile is one file of a TreeCase.
type TreeFile struct {
	// Path is the file's slash-separated path below the case's directory.
	Path string
	// Data is the file's content, byte for byte.
	Data []byte
}

// Target returns the directory the case is built from, relative to the
// archive's root: the case's own directory, or the directory its .target
// file names, relative to that.
func (c TreeCase) Target() string {
	for _, f := range c.Files {
		if f.Path == targetFile {
			return path.Join(c.Name, strings.TrimSpace(string(f.Data)))
		}
	}
	return c.Name
}

// stdDir is the directory, within a tree case, that holds the case's own
// standard library, if it has one. Its name starts with a dot, so that no
// import path of the case's module can name it by accident.
const stdDir = ".std"

// StdDir returns the directory of the case's own standard library,
// relative to the archive's root, or "" when the case has none and is
// built with the repository's: it has one when any of its files is under
// its .std directory.
func (c TreeCase) StdDir() string {
	for _, f := range c.Files {
		if strings.HasPrefix(f.Path, stdDir+"/") {
			return path.Join(c.Name, stdDir)
		}
	}
	return ""
}

// LoadTree reads the archive of tree cases at path and parses it. It fails
// if the archive holds no cases, as Load does.
func LoadTree(path string) ([]TreeCase, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the archive by design
	if err != nil {
		return nil, fmt.Errorf("reading invalid examples: %w", err)
	}
	_, cases, err := ParseTree(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: no cases", path)
	}
	return cases, nil
}

// ParseTree splits an archive of tree cases into its leading comment and
// its cases. It returns an error naming the offending line for any
// structure the package doc rules out.
func ParseTree(data []byte) (comment []byte, cases []TreeCase, err error) {
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, nil, fmt.Errorf("archive does not end with a newline")
	}
	comment, files, err := split(data)
	if err != nil {
		return nil, nil, err
	}
	for len(files) > 0 {
		var c TreeCase
		c, files, err = takeTreeCase(files)
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

// takeTreeCase reads the tree case that starts at files[0] and returns it
// with the files that follow it.
func takeTreeCase(files []file) (TreeCase, []file, error) {
	name, _, _ := strings.Cut(files[0].name, "/")
	if checkName(name) != nil || !strings.Contains(files[0].name, "/") {
		return TreeCase{}, nil, fmt.Errorf("line %d: want a header -- <case>/<file> --, got %q",
			files[0].line, files[0].name)
	}
	c := TreeCase{Name: name}
	seen := make(map[string]bool)
	for i, f := range files {
		rel, ok := strings.CutPrefix(f.name, name+"/")
		if !ok {
			return TreeCase{}, nil, fmt.Errorf("line %d: case %q ends without its %s file",
				f.line, name, name+expectedSuffix)
		}
		if rel == expectedSuffix[1:] {
			return finishTreeCase(c, f, files[i+1:])
		}
		if err := checkTreePath(rel); err != nil || seen[rel] {
			return TreeCase{}, nil, fmt.Errorf("line %d: case %q: bad or repeated file %q",
				f.line, name, rel)
		}
		seen[rel] = true
		c.Files = append(c.Files, TreeFile{Path: rel, Data: f.data})
	}
	return TreeCase{}, nil, fmt.Errorf("line %d: case %q ends without its %s file",
		files[len(files)-1].line, name, name+expectedSuffix)
}

// finishTreeCase completes c with its expected output, exp, and returns it
// with rest, the files after it.
func finishTreeCase(c TreeCase, exp file, rest []file) (TreeCase, []file, error) {
	if len(c.Files) == 0 {
		return TreeCase{}, nil, fmt.Errorf("line %d: case %q has no files", exp.line, c.Name)
	}
	if len(exp.data) == 0 {
		return TreeCase{}, nil, fmt.Errorf(
			"line %d: case %q has an empty expected output", exp.line, c.Name)
	}
	c.Expected = exp.data
	return c, rest, nil
}

// checkTreePath reports whether rel can be the path of a file in a tree
// case: clean, relative, and not the expected output's name.
func checkTreePath(rel string) error {
	if rel == "" || path.Clean(rel) != rel || path.IsAbs(rel) || strings.HasPrefix(rel, "..") ||
		strings.ContainsAny(rel, " \t\r\n") || rel == expectedSuffix[1:] {
		return fmt.Errorf("invalid file path %q", rel)
	}
	return nil
}

// FormatTree renders comment and cases as an archive that ParseTree turns
// back into the same comment and cases. It fails, naming the case, for
// anything the format cannot hold exactly.
func FormatTree(comment []byte, cases []TreeCase) ([]byte, error) {
	if err := checkContent("the comment", comment); err != nil {
		return nil, err
	}
	if len(comment) > 0 && comment[len(comment)-1] != '\n' {
		return nil, fmt.Errorf("the comment does not end with a newline")
	}
	var buf bytes.Buffer
	buf.Write(comment)
	for i, c := range cases {
		if err := checkTreeCase(c); err != nil {
			return nil, err
		}
		if i > 0 && cases[i-1].Name >= c.Name {
			return nil, fmt.Errorf(
				"case %q comes after %q: cases must be in strictly increasing order",
				c.Name,
				cases[i-1].Name,
			)
		}
		for _, f := range c.Files {
			fmt.Fprintf(&buf, "%s%s/%s%s\n", headerStart, c.Name, f.Path, headerEnd)
			buf.Write(f.Data)
		}
		fmt.Fprintf(&buf, "%s%s%s%s\n", headerStart, c.Name, expectedSuffix, headerEnd)
		buf.Write(c.Expected)
	}
	return buf.Bytes(), nil
}

// checkTreeCase reports why c cannot be written to an archive, if it
// cannot: every file must end with a newline, since the format has no
// marker for one that does not; such a program belongs in Path's archive.
func checkTreeCase(c TreeCase) error {
	if err := checkName(c.Name); err != nil {
		return err
	}
	if err := checkTreeFiles(c); err != nil {
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

// checkTreeFiles reports why the files of c cannot be written to an
// archive, if they cannot.
func checkTreeFiles(c TreeCase) error {
	if len(c.Files) == 0 {
		return fmt.Errorf("case %q has no files", c.Name)
	}
	seen := make(map[string]bool)
	for _, f := range c.Files {
		if err := checkTreePath(f.Path); err != nil || seen[f.Path] {
			return fmt.Errorf("case %q: bad or repeated file %q", c.Name, f.Path)
		}
		seen[f.Path] = true
		what := c.Name + "/" + f.Path
		if err := checkContent(what, f.Data); err != nil {
			return err
		}
		if len(f.Data) > 0 && f.Data[len(f.Data)-1] != '\n' {
			return fmt.Errorf("%s does not end with a newline", what)
		}
	}
	return nil
}
