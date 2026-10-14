package codegen_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/diag"
)

// fakeBackend is a stand-in backend used to exercise the registry without
// depending on what the real backends emit.
type fakeBackend struct{ name string }

func (f fakeBackend) Name() string { return f.name }

func (f fakeBackend) Ext() string { return ".fake" }

func (f fakeBackend) Generate(
	*ast.Program,
) ([]byte, []diag.Diagnostic) {
	return []byte("fake"), nil
}

func (f fakeBackend) Header(*ast.Program) []byte { return nil }

func (f fakeBackend) CompileCmd(src, _, obj string) []string { return []string{"true", src, obj} }

func (f fakeBackend) BuildCmd(src string, _ []string, out string) []string {
	return []string{"true", src, out}
}

func TestGetReturnsRegisteredBackend(t *testing.T) {
	codegen.Register(fakeBackend{"registry-test"})

	got, ok := codegen.Get("registry-test")
	if !ok {
		t.Fatalf("Get(%q) = _, false, want the backend that was just registered", "registry-test")
	}
	if got.Name() != "registry-test" {
		t.Errorf("Name() = %q, want %q", got.Name(), "registry-test")
	}
}

func TestGetReportsUnknownBackend(t *testing.T) {
	if _, ok := codegen.Get("no-such-backend"); ok {
		t.Error("Get(\"no-such-backend\") = _, true, want false")
	}
}

func TestNamesListsRegisteredBackendsSorted(t *testing.T) {
	codegen.Register(fakeBackend{"zzz-names-test"})
	codegen.Register(fakeBackend{"aaa-names-test"})

	names := codegen.Names()

	first, second := -1, -1
	for i, n := range names {
		switch n {
		case "aaa-names-test":
			first = i
		case "zzz-names-test":
			second = i
		}
	}
	if first == -1 || second == -1 {
		t.Fatalf("Names() = %q, want it to contain both registered backends", names)
	}
	if first > second {
		t.Errorf("Names() = %q, want sorted order", names)
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering two backends under one name did not panic")
		}
	}()

	codegen.Register(fakeBackend{"dup-test"})
	codegen.Register(fakeBackend{"dup-test"})
}
