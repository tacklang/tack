package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/loader"
)

// std is the standard library a run uses: where its packages are. The
// builtins and the runtime are the compiler's own, not std's (see package
// builtin), so nothing in it changes them.
type std struct {
	dir string // $TACK_HOME/src/std, absolute
}

// builtinSet returns the builtins a run with s uses, the compiler's, or
// none when s is nil, as it is for a dump that needs none.
func (s *std) builtinSet() *builtin.Set {
	if s == nil {
		return nil
	}
	return builtin.Default()
}

// loaderStd returns where the loader finds the packages of s, read from
// the host's file system as the program's are, nil when s is nil.
func (s *std) loaderStd() *loader.Std {
	if s == nil {
		return nil
	}
	return &loader.Std{Dir: filepath.ToSlash(s.dir)}
}

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
// ~/.tack/src/std when TACK_HOME is unset. A std that is not there is an
// error saying how to put it there. Nothing in it is read here: its
// packages are loaded as a program imports them, and its std/builtin, a
// copy of the builtins' documentation, is never read.
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
	return &std{dir: dir}, nil
}
