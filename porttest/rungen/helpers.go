package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"os"
	"slices"
	"sort"
	"strings"
)

// modelled are the Go test helpers rungen does not interpret but models:
// it knows what each builds or asserts and writes the record lines for
// that (see builtReader and sanReader), so a change to one would change
// what a record must say without rungen seeing it; and the variables
// whose values rungen and the runners hold copies of rather than read,
// as strictCFlags. Each is listed by its package's directory and name,
// with the SHA-256 of its source, a function's declaration from func to
// its closing brace or a variable's spec, and rungen stops when one
// differs (see checkModelled). The helpers rungen interprets instead,
// statement by statement, as runArithmeticExample, runBig, printfExample
// and program, are not listed: what they do is read from them each time.
var modelled = map[string]string{
	// tack/: building an example with the driver and running it.
	"tack buildWithClang":       "641785d0a1b15daca4ee78783cad3f45aace9ed510ee09c52b0824c371c5bbd8",
	"tack buildTargetWithClang": "e73efae8fc44037d20b2faa18de795d930fd3a7a9cb789e90faed1b379e84fba",
	"tack example":              "f5274264096b3fe6103c1f02d91af2778fb31ec358c33eafe4cba55e36725612",
	"tack runBuilt":             "22f2e1fff5e35f97f062327d3be3751e736faf60a63006291d7cbfe4c91e2454",
	// codegen/c: the front end and the builds with clang flags of the
	// test's own, and the runs and their assertions.
	"codegen/c atO0Too":           "f4f2476e39755a040b53885603f255d5656b649a83b057299ffe4c80bca7acd1",
	"codegen/c isLevel":           "48b92bb7b2ea1fee667d7dd6ce246f86eee63c334c2bf57a686c841a441ec83b",
	"codegen/c readFile":          "67b012a0a057ece6449b1aa6dfed0664008bca1cd865cc7d9349dd73138742e0",
	"codegen/c exampleFiles":      "1e0d4e4d246ca1ac46b343c0ee7e83b0e2e539abdf313b868672448f08570645",
	"codegen/c compile":           "88166ece08577883256b20c6aa1e3507fbb6ad77b430fb5725d98617f685b7b4",
	"codegen/c compileWithStd":    "958a2378a4d8a9b6e3ab58a70a26e82b19b3c1d04cde42418c433e032edca59f",
	"codegen/c compileExampleDir": "7e99932e66f57cf511dd35a142bfe45e418f545f98dc9ac37ed39ffc9cb2e1ab",
	"codegen/c generate":          "21022619c7d2ad32486cdb327d660f476d79750059606326cfe7049240f36c68",
	"codegen/c buildWith":         "0f6614d165ec299dd444d5905019df8b5b91a4143d4ce2d8d7ce085308c3f688",
	"codegen/c buildCWith":        "e9af25b5c59c78c4ece01c64c59ff2495821f027305d468204fbc8150d7dd291",
	"codegen/c buildWithStd":      "2a8e27a1f26752246bb4f2bcb5dff311326c8e7cc5e4df4a89c8060fbda4b81a",
	"codegen/c buildWithStdAndC":  "38e6ec679fcc4d5ecf83d06d6de8a5f3e0b671a72b2b7ed0827c9ae1d9a66568",
	"codegen/c buildProgram":      "6fbe44f965e6e019a8564c1f1014ba027b0216161b27312d0ec2f719cbc5946f",
	"codegen/c buildExternWith":   "1f797309b7fa94232c6640fab603f504120677cbe4eac80a38901156dd36bf36",
	"codegen/c runChecks":         "22ed36353b36353e40657f71e98c83ecf5e949c7c24eac26f173b7a3c0dc86b5",
	"codegen/c runRegionChecks":   "a6d4c81f2b719d3dd7d48d14b832cc79ba0d88e7235b8d68f1c27bb51d7d9ece",
	"codegen/c leakChecked":       "3ffea531bf17abceaa7f67ac46b67755c45d32c4de0c556aa8d86dec8e34ea2d",
	"codegen/c runExample":        "c66ea2d4824abe355aa47a5d9c10f2fcea4385e67501fe962441d5141c1f85ed",
	"codegen/c runStatus":         "fc9f70077d96b77fac8954d3db8cceed3159468582353b17c278fda9d87c5bc6",
	"codegen/c checkWrites":       "816df7269ebe54693e5a36bca2bbb52f73e4d2a484ca4704b8bc5b9de6a884e3",
	// codegen/c's strict flags, which sanStrictFlags, the port's
	// strict_c_flags and porttest's strictCFlags copy.
	"codegen/c strictCFlags": "2c63a4ad5976fff5bacfe4c5f28234f1b7745e367e8a7877d79ec9caf5fa81f8",
}

// helperSource returns the source of the function or package-level
// variable name of the test files of the package dir, and the file it is
// in, stopping rungen when no file or more than one declares it.
func helperSource(dir, name string) (string, string) {
	var found, file string
	for _, f := range packageFiles(dir + "/x_test.go") {
		s := src(f)
		for _, d := range s.file.Decls {
			var node ast.Node
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil && x.Name.Name == name {
					node = x
				}
			case *ast.GenDecl:
				for _, spec := range x.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && slices.ContainsFunc(vs.Names, func(n *ast.Ident) bool { return n.Name == name }) {
						node = vs
					}
				}
			}
			if node == nil {
				continue
			}
			if found != "" {
				fail(fmt.Errorf("%s declares %s twice", dir, name))
			}
			data, err := os.ReadFile(f)
			if err != nil {
				fail(err)
			}
			// src parses each file in a set of its own, whose positions are
			// the file's offsets plus one.
			found, file = string(data[int(node.Pos())-1:int(node.End())-1]), f
		}
	}
	if found == "" {
		fail(fmt.Errorf("%s has no helper %s, which rungen models", dir, name))
	}
	return found, file
}

// fingerprint returns the SHA-256 of s in hex.
func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// checkModelled stops rungen when a helper of modelled differs from the
// one rungen models, by its fingerprint.
func checkModelled() {
	keys := make([]string, 0, len(modelled))
	for k := range modelled {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dir, name, _ := strings.Cut(k, " ")
		source, file := helperSource(dir, name)
		if got := fingerprint(source); got != modelled[k] {
			fail(fmt.Errorf("helper %s of %s differs from the one rungen models (fingerprint %s, want %s): "+
				"read the change, make rungen model what it does now, and update modelled", name, file, got, modelled[k]))
		}
	}
}

// printFingerprints writes, for rungen -fingerprints, each helper of
// modelled with the fingerprint of its source now, as modelled lists them.
func printFingerprints() {
	keys := make([]string, 0, len(modelled))
	for k := range modelled {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dir, name, _ := strings.Cut(k, " ")
		source, _ := helperSource(dir, name)
		fmt.Printf("\t%q: %q,\n", k, fingerprint(source))
	}
}
