package builtin_test

import (
	"os"
	"strings"
	"testing"

	"github.com/tacklang/tack/builtin"
)

// A generic builtin has no declaration in the documentation of record, so
// it must at least be described there, as len(...) in a comment.
func TestGenericBuiltinsAreDocumented(t *testing.T) {
	data, err := os.ReadFile(declarationsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", declarationsPath, err)
	}
	for _, name := range builtin.Generic() {
		if !strings.Contains(string(data), "// "+name+"(") {
			t.Errorf("%s does not describe the builtin %s", declarationsPath, name)
		}
	}
}

// Every builtin's name is in Names, so the resolver can bind a call to it.
func TestNamesListsEveryBuiltin(t *testing.T) {
	names := strings.Join(builtin.Names(), " ")
	for _, want := range []string{"print", "exit", "len"} {
		if !strings.Contains(" "+names+" ", " "+want+" ") {
			t.Errorf("Names() = %q, missing %s", names, want)
		}
	}
}
