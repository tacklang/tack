package modfile_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/modfile"
	"github.com/tacklang/tack/source"
)

// parse parses src as a tack.mod and returns the File and the rendered
// diagnostics, one per line.
func parse(src string) (*modfile.File, string) {
	f := &source.File{ID: "tack.mod", Filename: "tack.mod", Data: []byte(src)}
	mod, diags := modfile.Parse(f)
	return mod, diag.RenderAll(f, diags)
}

// TestParseAccepts checks the forms of a valid tack.mod: comments, blank
// lines, spaces and tabs, CRLF, no final newline, and names using every
// character a part allows.
func TestParseAccepts(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{"module calc\n", "calc"},
		{"module calc", "calc"},
		{"// tack.mod\n\n\tmodule   calc  // the module\n// end\n", "calc"},
		{"module calc\r\n", "calc"},
		{"module example.com/my-tools/x_y.2\n", "example.com/my-tools/x_y.2"},
		{"module a\n", "a"},
		{"module 7\n", "7"},
		{"module stdlib\n", "stdlib"},
		{"module x/std\n", "x/std"},
		{"module a//b\n", "a"}, // // starts a comment even inside a word
		{"module " + strings.Repeat("a", 10000) + "\n", strings.Repeat("a", 10000)},
	} {
		mod, diags := parse(tt.src)
		if diags != "" || mod == nil || mod.Module != tt.want {
			t.Errorf("Parse(%q) = %v, %q; want module %s", tt.src, mod, diags, tt.want)
		}
	}
}

// TestParseRejects checks every malformed tack.mod is reported, at the
// word it is about, with no File returned.
func TestParseRejects(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"empty", "", "error: tack.mod:1:1: (0) tack.mod declares no module; write module <name>"},
		{"comments only", "// x\n", "tack.mod declares no module"},
		{"no name", "module\n", "tack.mod:1:1: (0) module needs a name: module <name>"},
		{"no name before comment", "module // calc\n", "module needs a name"},
		{"two names", "module a b\n", "tack.mod:1:10: (9) unexpected b after the module name"},
		{"two lines", "module a\nmodule b\n", "tack.mod:2:1: (9) module is already declared"},
		{"unknown directive", "require x v1\n", "unknown directive require; tack.mod holds only a module line"},
		{"capital directive", "Module calc\n", "unknown directive Module"},
		{"leading dash", "module -x\n", "invalid module name -x: a part cannot start or end with . or -"},
		{"trailing dot", "module x.\n", "invalid module name x.: a part cannot start or end with . or -"},
		{"dot part", "module a/./b\n", "a part cannot start or end with . or -"},
		{"dot dot part", "module a/../b\n", "a part cannot start or end with . or -"},
		{"leading slash", "module /a\n", "a part between slashes is empty"},
		{"trailing slash", "module a/\n", "a part between slashes is empty"},
		{"uppercase", "module Calc\n", "invalid module name Calc: use lowercase letters, digits, -, . and _"},
		{"odd byte", "module ca\xfflc\n", `invalid module name "ca\xfflc": use lowercase`},
		{"lone carriage return", "module ca\rlc\n", `invalid module name "ca\rlc"`},
		{"tilde", "module a~b\n", "use lowercase letters"},
		{"std", "module std\n", "module name std is reserved for the standard library"},
		{"std path", "module std/x\n", "module name std/x starts with std, which is reserved"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mod, diags := parse(tt.src)
			if mod != nil || !strings.Contains(diags, tt.want) {
				t.Errorf(
					"Parse(%q) = %v, %q; want an error containing %q",
					tt.src,
					mod,
					diags,
					tt.want,
				)
			}
		})
	}
}

// TestParseReportsEveryLine checks one run reports a problem on each line
// that has one.
func TestParseReportsEveryLine(t *testing.T) {
	_, diags := parse("bogus\nmodule A\nmodule b\n")
	if n := strings.Count(diags, "error:"); n != 3 {
		t.Errorf("got %d errors, want 3:\n%s", n, diags)
	}
}

// FuzzParse checks Parse never panics, gives a File exactly when it gives
// no diagnostic, gives only a name CheckModule accepts, and every
// diagnostic's span lies within the file and renders.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"module calc\n", "", "module\n", "module a b\n", "module a\nmodule b\n",
		"// c\nmodule x // c\r\n", "module std\n", "module a//b\n", "module \xff\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file := &source.File{ID: "tack.mod", Filename: "tack.mod", Data: data}
		mod, diags := modfile.Parse(file)
		if (mod == nil) == (len(diags) == 0) {
			t.Fatalf("File %v with %d diagnostics", mod, len(diags))
		}
		if mod != nil && modfile.CheckModule(mod.Module) != "" {
			t.Fatalf("accepted a bad name %q", mod.Module)
		}
		for _, d := range diags {
			if d.Span.Start < 0 || d.Span.End > len(data) || d.Span.Start > d.Span.End {
				t.Fatalf("span %v outside %d bytes", d.Span, len(data))
			}
			_ = diag.RenderPretty(file, d)
		}
	})
}
