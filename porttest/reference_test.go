package porttest

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReferenceOverlay checks referenceOverlay on small trees, needing
// no compiler: builtin.tck and tack.h map to the port's, a C file both
// have and one only the port has map to the port's, one only the frozen
// tree has maps to "", go build's deletion, and a port runtime without
// tack.h is an error.
func TestReferenceOverlay(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	write := func(p string) {
		mustDo(t, os.MkdirAll(filepath.Dir(p), 0o755))
		mustDo(t, os.WriteFile(p, []byte(p), 0o600))
	}
	frozen := filepath.Join(root, "builtin", "runtime")
	port := filepath.Join(src, portRuntimeDir)
	for _, p := range []string{
		filepath.Join(root, "builtin", "builtin.tck"), filepath.Join(frozen, "tack", "tack.h"),
		filepath.Join(frozen, "both.c"), filepath.Join(frozen, "gone.c"),
		filepath.Join(port, "builtin.tck"), filepath.Join(port, "both.c"),
		filepath.Join(port, "new.c"), filepath.Join(port, "tack.mod"),
	} {
		write(p)
	}
	if _, err := referenceOverlay(root, src); err == nil {
		t.Fatal("no error for a port runtime without tack/tack.h")
	}
	write(filepath.Join(port, "tack", "tack.h"))
	data, err := referenceOverlay(root, src)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Replace map[string]string }
	mustDo(t, json.Unmarshal(data, &got))
	want := map[string]string{
		filepath.Join(root, "builtin", "builtin.tck"): filepath.Join(port, "builtin.tck"),
		filepath.Join(frozen, "tack", "tack.h"):       filepath.Join(port, "tack", "tack.h"),
		filepath.Join(frozen, "both.c"):               filepath.Join(port, "both.c"),
		filepath.Join(frozen, "new.c"):                filepath.Join(port, "new.c"),
		filepath.Join(frozen, "gone.c"):               "",
	}
	if !maps.Equal(got.Replace, want) {
		t.Errorf("overlay %v, want %v", got.Replace, want)
	}
	write(filepath.Join(port, "notes.txt"))
	if _, err := referenceOverlay(root, src); err == nil {
		t.Error("no error for a port runtime holding a file the reference would not embed")
	}
}

// TestCheckReference checks checkReference on a small port tree, needing
// no compiler: a binary holding every embedded file passes, and one
// lacking one fails, naming it.
func TestCheckReference(t *testing.T) {
	src := t.TempDir()
	port := filepath.Join(src, portRuntimeDir)
	files := map[string]string{
		"builtin.tck": "// builtins\n", "tack/tack.h": "// header\n",
		"a.c": "int a(void) { return 1; }\n", "b.c": "int b(void) { return 2; }\n",
	}
	for rel, data := range files {
		p := filepath.Join(port, filepath.FromSlash(rel))
		mustDo(t, os.MkdirAll(filepath.Dir(p), 0o755))
		mustDo(t, os.WriteFile(p, []byte(data), 0o600))
	}
	bin := filepath.Join(t.TempDir(), "tack-go")
	all := "\x7fELF" + files["builtin.tck"] + "\x00" + files["tack/tack.h"] + "\x00" +
		files["a.c"] + "\x00" + files["b.c"]
	mustDo(t, os.WriteFile(bin, []byte(all), 0o600))
	if err := checkReference(bin, src); err != nil {
		t.Errorf("a binary holding every file: %v", err)
	}
	lacking := strings.Replace(all, files["b.c"], "int b(void) { return 3; }\n", 1)
	mustDo(t, os.WriteFile(bin, []byte(lacking), 0o600))
	err := checkReference(bin, src)
	if err == nil || !strings.Contains(err.Error(), portRuntimeDir+"/b.c") {
		t.Errorf("a binary lacking b.c: %v, want an error naming %s/b.c", err, portRuntimeDir)
	}
}
