package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// cacheFormat names the layout of the cache and of its keys. It is the
// first thing every key hashes, so changing it, as a change to either
// must, leaves every older entry unused rather than misread.
const cacheFormat = "tack std object cache 1"

// objPlaceholder stands for the object's path in the compile command a
// key hashes: the path is a fresh temporary one in every build, and where
// the object is written does not change what it holds.
const objPlaceholder = "<object>"

// maxTreeDepth bounds how deep hashTree descends, so that a symbolic link
// to a directory above it ends the walk with an error instead of never.
const maxTreeDepth = 64

// objCache is the part of the build cache that holds the objects compiled
// from std's C files, content-addressed: keys/<key> holds the hex SHA-256
// of an object's bytes, and objects/<that hash>.o the object. A key is
// the hash of everything that decides what the object holds (stdKey), so
// a change to any of it is a new key, never a stale hit. Every file is
// written whole to a temporary file beside it and renamed into place, so
// a build running at the same time sees an entry whole or not at all, and
// an object is written before the key that names it.
type objCache struct {
	dir string // <cache>/std, absolute
}

// openCache returns the cache env names, $TACK_CACHE, or the tack
// directory in the user's cache directory when TACK_CACHE is unset or
// empty, creating its directories when they are not there. A cache that
// cannot be found or created is an error naming it: a build that needs
// the cache does not go on without one.
func openCache(env environment) (*objCache, error) {
	root := env.tackCache
	if root == "" {
		user, err := env.userCacheDir()
		if err != nil {
			return nil, fmt.Errorf("cannot find the build cache: "+
				"TACK_CACHE is not set and the user cache directory is unknown: %w", err)
		}
		root = filepath.Join(user, "tack")
	}
	// Made absolute once, as TACK_HOME is, so a relative TACK_CACHE is
	// taken from the current directory.
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("cannot find the build cache: %w", err)
	}
	c := &objCache{dir: filepath.Join(root, "std")}
	for _, sub := range []string{"keys", "objects"} {
		if err := os.MkdirAll(filepath.Join(c.dir, sub), 0o750); err != nil {
			return nil, fmt.Errorf("cannot use the build cache %s: %w", c.dir, err)
		}
	}
	return c, nil
}

// keyPath returns the path of the file the key key is stored in.
func (c *objCache) keyPath(key string) string { return filepath.Join(c.dir, "keys", key) }

// objectPath returns the path of the object whose hex SHA-256 is sum.
func (c *objCache) objectPath(sum string) string {
	return filepath.Join(c.dir, "objects", sum+".o")
}

// get writes the object cached under key to dst and reports whether there
// was one. An entry that is absent, unreadable, or damaged, a key that is
// not a hash or an object whose bytes do not hash to it, is a miss, for
// the caller to compile and put again; only failing to write dst is an
// error.
func (c *objCache) get(key, dst string) (bool, error) {
	ref, err := os.ReadFile(c.keyPath(key)) //nolint:gosec // G304: key is a hex hash, in the cache
	if err != nil {
		return false, nil //nolint:nilerr // an unreadable entry is a miss, rebuilt
	}
	sum, ok := parseSum(ref)
	if !ok {
		return false, nil
	}
	data, err := os.ReadFile(c.objectPath(sum)) //nolint:gosec // G304: parseSum checked sum is hex
	if err != nil || hexSum(data) != sum {
		return false, nil //nolint:nilerr // likewise
	}
	return true, writeFile(dst, data)
}

// put caches the object at src under key: the object first, then the key
// naming it, each written whole and renamed into place, replacing any
// entry there, a damaged one included.
func (c *objCache) put(key, src string) error {
	data, err := os.ReadFile(src) //nolint:gosec // G304: src is the build's own object
	if err != nil {
		return fmt.Errorf("failed to read temp file: %w", err)
	}
	sum := hexSum(data)
	if err := c.write(c.objectPath(sum), data); err != nil {
		return err
	}
	return c.write(c.keyPath(key), []byte(sum+"\n"))
}

// write writes data to a temporary file in the directory of path and
// renames it to path, so that no reader ever sees path half written. The
// temporary file is removed on every failure.
func (c *objCache) write(path string, data []byte) (err error) {
	fail := func(err error) error {
		return fmt.Errorf("cannot write to the build cache %s: %w", c.dir, err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fail(err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(f.Name()) // best effort; the write's error is the one to report
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // the write's error is the one to report
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fail(err)
	}
	return nil
}

// parseSum returns the hex SHA-256 a key file holds, ref, which is that,
// in lower case, and a newline, and reports whether ref is that. Only
// such a sum names an object, so a damaged key never names a path outside
// the cache's objects.
func parseSum(ref []byte) (string, bool) {
	sum, ok := bytes.CutSuffix(ref, []byte("\n"))
	if !ok || len(sum) != 2*sha256.Size {
		return "", false
	}
	for _, ch := range sum {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return "", false
		}
	}
	return string(sum), true
}

// hexSum returns the hex SHA-256 of data, in lower case.
func hexSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// keyHash builds a cache key: a SHA-256 over a sequence of fields, each
// written after its length, so no two different sequences hash the same
// bytes.
type keyHash struct{ h hash.Hash }

// newKeyHash returns an empty keyHash.
func newKeyHash() keyHash { return keyHash{h: sha256.New()} }

// field adds b to the hash as one field.
func (k keyHash) field(b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	k.h.Write(n[:]) // a hash.Hash never fails to write
	k.h.Write(b)
}

// str adds s to the hash as one field.
func (k keyHash) str(s string) { k.field([]byte(s)) }

// sum returns the hash of the fields added so far, in hex.
func (k keyHash) sum() string { return hex.EncodeToString(k.h.Sum(nil)) }

// stdKey returns the key of the object the C file of std at src compiles
// to in this build: the hash of the cache's format, the backend's name, the
// command CompileStdCmd gives for src, with the object's path left out
// (which holds src's own absolute path, std's include directory and the
// optimization level), toolchain, the output of the backend's VersionCmd,
// include, the hash of std's include directory, the bytes of src, and the
// name and bytes of every .h file beside it, which an #include "x.h" in
// it finds. The headers of the C library are not hashed: the toolchain's
// identity stands for them.
func (b *builder) stdKey(src string, toolchain, include []byte) (string, error) {
	k := newKeyHash()
	k.str(cacheFormat)
	k.str(b.backend.Name())
	cmd := b.backend.CompileStdCmd(src, b.std.includeDir(), objPlaceholder, b.opt)
	k.str(strconv.Itoa(len(cmd)))
	for _, arg := range cmd {
		k.str(arg)
	}
	k.field(toolchain)
	k.field(include)
	data, err := os.ReadFile(src) //nolint:gosec // G304: reading std is the point
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", src, err)
	}
	k.field(data)
	if err := hashHeaders(k, filepath.Dir(src)); err != nil {
		return "", fmt.Errorf("cannot read the headers beside %s: %w", src, err)
	}
	return k.sum(), nil
}

// hashHeaders adds to k the name and bytes of every .h file in dir, in
// name order, following symbolic links as the compiler does.
func hashHeaders(k keyHash, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err //nolint:wrapcheck // the caller says what was being read
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".h") {
			continue
		}
		if err := hashEntry(k, filepath.Join(dir, e.Name()), e.Name(), -1); err != nil {
			return err
		}
	}
	return nil
}

// hashTree returns the hash of the tree at root, std's include directory:
// every file and directory in it, by its path below root, and every
// file's bytes, following symbolic links as the compiler does. A root that
// does not exist hashes as an empty tree, as a std without an include
// directory compiles: there is nothing in it to include.
func hashTree(root string) ([]byte, error) {
	k := newKeyHash()
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		k.str("absent")
		return k.h.Sum(nil), nil
	}
	if err := hashDir(k, root, ".", 0); err != nil {
		return nil, fmt.Errorf("cannot read std's include directory %s: %w", root, err)
	}
	return k.h.Sum(nil), nil
}

// hashDir adds to k every entry of the directory dir, whose path below the
// tree's root is rel, in name order, descending into directories, depth
// levels below the root.
func hashDir(k keyHash, dir, rel string, depth int) error {
	if depth > maxTreeDepth {
		return fmt.Errorf("%s is nested more than %d directories deep, "+
			"or a symbolic link leads to a directory above it", dir, maxTreeDepth)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err //nolint:wrapcheck // the caller says what was being read
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if err := hashEntry(
			k,
			path,
			filepath.ToSlash(filepath.Join(rel, e.Name())),
			depth,
		); err != nil {
			return err
		}
	}
	return nil
}

// hashEntry adds to k the entry at path, named name in the hash: a file's
// bytes, a directory's entries when depth is not negative and its name
// alone otherwise, and the kind alone of anything else, such as a
// symbolic link to nothing, which no #include can read.
func hashEntry(k keyHash, path, name string, depth int) error {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		k.str("dangling " + name)
		return nil
	case err != nil:
		return err //nolint:wrapcheck // the caller says what was being read
	case info.Mode().IsRegular():
		data, err := os.ReadFile(path) //nolint:gosec // G304: reading std is the point
		if err != nil {
			return err //nolint:wrapcheck // likewise
		}
		k.str("file " + name)
		k.field(data)
	case info.IsDir():
		k.str("dir " + name)
		if depth >= 0 {
			return hashDir(k, path, name, depth+1)
		}
	default:
		k.str("other " + name)
	}
	return nil
}

// toolchainVersion runs argv, a backend's VersionCmd, and returns its
// standard output. A command that cannot be run or fails is an error
// naming it, with what it wrote to standard error.
func toolchainVersion(ctx context.Context, argv []string) ([]byte, error) {
	// The command is the backend's own, as runTool's are.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // see above
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s failed: %w: %s", strings.Join(argv, " "), err, msg)
		}
		return nil, fmt.Errorf("%s failed: %w", strings.Join(argv, " "), err)
	}
	return out, nil
}
