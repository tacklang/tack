package resolver_test

import (
	"strings"
	"testing"
)

// TestIncDecIsNotARead checks x++ and x-- do not count as using x, and that
// their target is resolved.
func TestIncDecIsNotARead(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			"only incremented",
			"package main\n\nfunc main() {\n    mut x = 0\n    x++\n    x--\n}\n",
			[]string{"declared and not used: x"},
		},
		{
			"incremented and read",
			"package main\n\nfunc main() {\n    mut x = 0\n    x++\n    exit(x)\n}\n",
			nil,
		},
		{"undefined", "package main\n\nfunc main() {\n    y++\n}\n", []string{"undefined: y"}},
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
