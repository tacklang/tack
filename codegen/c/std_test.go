package c_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// TestStdTackStringMatchesGenerated checks the tack_string std's C files
// are compiled against, in std/include/tack/tack.h, is the one the
// generated C defines, member for member, so a string passed to a builtin
// is laid out as its body reads it.
func TestStdTackStringMatchesGenerated(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(stdtest.IncludeDir(), "tack", "tack.h"))
	if err != nil {
		t.Fatal(err)
	}
	got := generate(t, program(`print("hi")`))
	start := strings.Index(got, "typedef struct {\n")
	end := strings.Index(got, "} tack_string;\n")
	if start < 0 || end < start {
		t.Fatalf("the generated C defines no tack_string:\n%s", got)
	}
	def := got[start : end+len("} tack_string;\n")]
	if !strings.Contains(string(data), def) {
		t.Errorf("std's tack.h does not define tack_string as the generated C does:\n%s\nwant:\n%s",
			data, def)
	}
	if strings.Count(string(data), "tack_string;") != 1 {
		t.Errorf("std's tack.h defines tack_string more than once:\n%s", data)
	}
}
