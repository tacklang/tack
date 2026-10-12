package resolver_test

import (
	"strings"
	"testing"
)

// TestCompoundAssignmentIsNotARead checks updating a variable in place does
// not count as using it, as a plain assignment does not, and that its target
// and value are resolved like a plain assignment's.
func TestCompoundAssignmentIsNotARead(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			"only updated",
			"package main\n\nfunc main() {\n    mut x = 0\n    x += 1\n    x *= 2\n}\n",
			[]string{"declared and not used: x"},
		},
		{
			"updated and read",
			"package main\n\nfunc main() {\n    mut x = 0\n    x += 1\n    exit(x)\n}\n",
			nil,
		},
		{
			"read in its own value",
			"package main\n\nfunc main() {\n    mut x = 1\n    x += x\n}\n",
			nil,
		},
		{
			"undefined target",
			"package main\n\nfunc main() {\n    y += 1\n}\n",
			[]string{"undefined: y"},
		},
		{
			"undefined in the value",
			"package main\n\nfunc main() {\n    mut x = 0\n    x -= y\n    exit(x)\n}\n",
			[]string{"undefined: y"},
		},
		{
			"only updated global",
			"package main\n\nmut total int\n\nfunc main() {\n    total += 1\n}\n",
			[]string{"declared and not used: total"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			if got := messagesOf(diags); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
