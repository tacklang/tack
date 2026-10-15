package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tacklang/tack/builtin"
)

// std is the standard library a run uses: where it is, and the builtins
// its std/builtin declares.
type std struct {
	builtins *builtin.Set
	dir      string // $TACK_HOME/src/std, absolute
}

// builtinSet returns the builtins s declares, nil when s is nil, as it is
// for a dump that needs none.
func (s *std) builtinSet() *builtin.Set {
	if s == nil {
		return nil
	}
	return s.builtins
}

// includeDir returns the directory std's C files are compiled with on the
// include path, where they find <tack/tack.h>.
func (s *std) includeDir() string { return filepath.Join(s.dir, "include") }

// stdFor returns the standard library a run that stops at d needs, found
// from env: none for a dump of the tokens or the syntax trees, which needs
// no builtins and so works with no std at all, and findStd's for anything
// else.
func stdFor(d dump, env environment) (*std, error) {
	if d == dumpLex || d == dumpAST {
		return nil, nil
	}
	return findStd(env)
}

// findStd finds the standard library env names, $TACK_HOME/src/std, or
// ~/.tack/src/std when TACK_HOME is unset, and loads the builtins its
// std/builtin declares. A std that is not there is an error saying how to
// put it there; a std/builtin whose Tack files are not builtins is
// reported as diagnostics in those files.
func findStd(env environment) (*std, error) {
	home := env.tackHome
	if home == "" {
		user, err := env.homeDir()
		if err != nil {
			return nil, fmt.Errorf(
				"cannot find std: TACK_HOME is not set and the home directory is unknown: %w", err)
		}
		home = filepath.Join(user, ".tack")
	}
	// The path is made absolute here, once, so every path into std the
	// toolchain is given is absolute, and none can be read as an option.
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("cannot find std: %w", err)
	}
	dir := filepath.Join(home, "src", "std")
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist) || err == nil && !info.IsDir():
		return nil, fmt.Errorf("cannot find std at %s; set TACK_HOME or run make install", dir)
	case err != nil:
		return nil, fmt.Errorf("cannot read std at %s: %w", dir, err)
	}

	set, err := builtin.Load(filepath.Join(dir, "builtin"))
	var diags *builtin.DiagnosticsError
	if errors.As(err, &diags) {
		return nil, &diagnosticsError{rendered: diags.Error()}
	}
	if err != nil {
		return nil, err //nolint:wrapcheck // the error names the directory already
	}
	return &std{dir: dir, builtins: set}, nil
}
