package ast

import (
	"encoding/json"
	"strings"
	"testing"
)

// recursiveFile builds the tree a resolved recursive function has: the call
// inside f's body names f, so the Ident's Def points back at the Func that
// contains it.
func recursiveFile() (*File, *Func) {
	fn := &Func{Name: "f", NameSpan: Span{Start: 5, End: 6}, Span: Span{Start: 0, End: 20}}
	callee := &Ident{Name: "f", Def: fn, Span: Span{Start: 10, End: 11}}
	fn.Body = []Stmt{&ExprStmt{X: &Call{Fn: callee, Span: Span{Start: 10, End: 13}}}}
	f := NewFile()
	f.Package = "main"
	f.Decls = append(f.Decls, fn)
	return f, fn
}

// TestStringRendersRecursiveTree checks that a tree with a cycle through
// Ident.Def still renders, and that Def is shown as a reference naming the
// declaration rather than as a copy of it.
func TestStringRendersRecursiveTree(t *testing.T) {
	f, _ := recursiveFile()
	out := f.String()
	if !json.Valid([]byte(out)) {
		t.Fatalf("String() is not valid JSON: %q", out)
	}
	var tree struct {
		Decls []struct {
			Body []struct {
				X struct {
					Fn struct {
						Def map[string]any
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatal(err)
	}
	def := tree.Decls[0].Body[0].X.Fn.Def
	if def["Kind"] != "Func" || def["Name"] != "f" {
		t.Errorf("Def = %v, want a reference with Kind Func and Name f", def)
	}
	if _, ok := def["Body"]; ok {
		t.Errorf("Def = %v, want a reference, not the whole declaration", def)
	}
}

// TestStringRendersEveryDeclKindAsReference checks the reference for each
// kind of declaration an Ident can name, and for a builtin and an
// unresolved name, both of which have no declaration to point at.
func TestStringRendersEveryDeclKindAsReference(t *testing.T) {
	for _, tc := range []struct {
		name string
		def  Decl
		want string
	}{
		{"func", &Func{Name: "g", NameSpan: Span{Start: 1, End: 2}}, `"Kind":"Func","Name":"g"`},
		{"param", &Param{Name: "p", NameSpan: Span{Start: 3, End: 4}}, `"Kind":"Param","Name":"p"`},
		{"var", &Var{Name: "v", NameSpan: Span{Start: 5, End: 6}}, `"Kind":"Var","Name":"v"`},
		{"type", &TypeDecl{Name: "T", NameSpan: Span{Start: 7, End: 8}}, `"Kind":"Type","Name":"T"`},
		{"nil type", (*TypeDecl)(nil), `"Def":null`},
		{"builtin", (*Func)(nil), `"Def":null`},
		{"unresolved", nil, `"Def":null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			js, err := json.Marshal(&Ident{Name: "x", Def: tc.def})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(js), tc.want) {
				t.Errorf("got %s, want it to contain %s", js, tc.want)
			}
		})
	}
}
