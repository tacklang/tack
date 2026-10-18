package modfile

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
)

// Name is the name of the root file, which marks a module's root
// directory.
const Name = "tack.mod"

// File is what a tack.mod declares.
type File struct {
	// Module is the module's name.
	Module string
	// ModuleSpan is where the name is written.
	ModuleSpan ast.Span
}

// field is one word of a line, and where it is.
type field struct {
	text  string
	start int
	end   int
}

// Parse reads the tack.mod f holds, recording its line starts in f so its
// diagnostics can be rendered. It returns the File, or every problem found
// with it and a nil File.
func Parse(f *source.File) (*File, []diag.Diagnostic) {
	p := parser{file: f}
	start := 0
	for start <= len(f.Data) {
		f.AddLineStart(start)
		end := bytes.IndexByte(f.Data[start:], '\n')
		if end < 0 {
			p.line(start, len(f.Data))
			break
		}
		p.line(start, start+end)
		start += end + 1
	}
	if p.mod == nil && len(p.diags) == 0 {
		p.errorf(0, 0, "%s declares no module; write module <name>", Name)
	}
	if len(p.diags) > 0 {
		return nil, p.diags
	}
	return p.mod, nil
}

// parser holds what Parse has found so far.
type parser struct {
	file  *source.File
	mod   *File
	diags []diag.Diagnostic
}

// line parses the line of f.Data in [start, end), without its newline.
func (p *parser) line(start, end int) {
	text := p.file.Data[start:end]
	text = bytes.TrimSuffix(text, []byte("\r"))
	if i := bytes.Index(text, []byte("//")); i >= 0 {
		text = text[:i]
	}
	fields := split(text, start)
	if len(fields) == 0 {
		return
	}
	if fields[0].text != "module" {
		p.errorf(fields[0].start, fields[0].end,
			"unknown directive %s; %s holds only a module line", show(fields[0].text), Name)
		return
	}
	p.module(fields)
}

// module parses a module line, whose words are fields.
func (p *parser) module(fields []field) {
	switch {
	case len(fields) == 1:
		p.errorf(fields[0].start, fields[0].end, "module needs a name: module <name>")
		return
	case len(fields) > 2:
		p.errorf(fields[2].start, fields[2].end,
			"unexpected %s after the module name; a module has one name", show(fields[2].text))
		return
	}
	name := fields[1]
	span := ast.Span{File: p.file.ID, Start: name.start, End: name.end}
	if p.mod != nil {
		p.diags = append(p.diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  "module is already declared",
			Span:     ast.Span{File: p.file.ID, Start: fields[0].start, End: fields[0].end},
			Related: []diag.Diagnostic{{
				Severity: diag.Note,
				Message:  "previous declaration of module",
				Span:     p.mod.ModuleSpan,
			}},
		})
		return
	}
	if msg := CheckModule(name.text); msg != "" {
		p.diags = append(p.diags, diag.Diagnostic{Severity: diag.Error, Message: msg, Span: span})
	}
	p.mod = &File{Module: name.text, ModuleSpan: span}
}

// errorf records an error covering [start, end).
func (p *parser) errorf(start, end int, format string, args ...any) {
	p.diags = append(p.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     ast.Span{File: p.file.ID, Start: start, End: end},
	})
}

// split returns the words of text, which starts at offset base: the runs
// of bytes other than spaces and tabs.
func split(text []byte, base int) []field {
	var fields []field
	i := 0
	for i < len(text) {
		if text[i] == ' ' || text[i] == '\t' {
			i++
			continue
		}
		j := i
		for j < len(text) && text[j] != ' ' && text[j] != '\t' {
			j++
		}
		fields = append(fields, field{text: string(text[i:j]), start: base + i, end: base + j})
		i = j
	}
	return fields
}

// CheckModule returns why name cannot be a module's name, or "" if it can.
// A module name is one or more parts joined by slashes; a part is
// lowercase ASCII letters, digits, -, . and _, and does not start or end
// with . or -. std is a name like any other: the standard library's own
// tack.mod says module std, and a checkout of it is checked as that module.
func CheckModule(name string) string {
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if msg := checkPart(part); msg != "" {
			return fmt.Sprintf("invalid module name %s: %s", show(name), msg)
		}
	}
	return ""
}

// checkPart returns why part cannot be a part of a module name, or "".
func checkPart(part string) string {
	if part == "" {
		return "a part between slashes is empty"
	}
	for i := range len(part) {
		if !moduleByte(part[i]) {
			return "use lowercase letters, digits, -, . and _"
		}
	}
	if edge(part[0]) || edge(part[len(part)-1]) {
		return "a part cannot start or end with . or -"
	}
	return ""
}

// moduleByte reports whether c may be in a part of a module name.
func moduleByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_'
}

// edge reports whether c may not start or end a part of a module name.
func edge(c byte) bool { return c == '.' || c == '-' }

// show returns s as a message writes it: as it is if every byte of it is
// printable ASCII, and Go-quoted otherwise, so an odd byte is visible.
func show(s string) string {
	for i := range len(s) {
		if s[i] < ' ' || s[i] > '~' {
			return strconv.Quote(s)
		}
	}
	return s
}
