package c_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// TestRefusesProgramsWithErrors checks that the backend generates nothing
// from a program it must never lower: one the type checker has not checked,
// and one a stage reported errors about, counted on the program or on its
// type table, though its tree is otherwise one the backend lowers.
func TestRefusesProgramsWithErrors(t *testing.T) {
	src := "package main\n\nfunc main() {\n    print(\"hi\")\n}\n"
	for _, tc := range []struct {
		name  string
		prog  func() *ast.Program
		inMsg string
	}{
		{"unchecked", func() *ast.Program {
			p := ast.SingleFile(compile(t, src))
			p.Types = nil
			return p
		}, "has not checked"},
		{"errors on the program", func() *ast.Program {
			p := ast.SingleFile(compile(t, src))
			p.Errors = 1
			return p
		}, "with 1 errors"},
		{"errors on the table", func() *ast.Program {
			f := compile(t, src)
			f.Types.Errors = 2
			return ast.SingleFile(f)
		}, "with 2 errors"},
		{"nil", func() *ast.Program { return nil }, "no program"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, diags := c.New().Generate(tc.prog(), stdtest.MustBuiltins())
			if out != nil {
				t.Errorf("generated %d bytes, want none", len(out))
			}
			if len(diags) != 1 || !strings.Contains(diags[0].Message, tc.inMsg) {
				t.Errorf("diagnostics = %+v, want one saying %q", diags, tc.inMsg)
			}
		})
	}
}
