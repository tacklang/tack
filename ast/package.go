package ast

import "github.com/tacklang/tack/types"

// Package is one package: the files of one directory, or the one file named
// on the command line, which all share its package clause. Package-level
// names are shared by every file of a package; each file is still a tree of
// its own, with its own spans and comments.
type Package struct {
	// Name is the name every file's package clause gives.
	Name string
	// Path identifies the package within the program: main for a program
	// built from a single file, otherwise where the loader found it.
	Path string
	// Module is the name of the module the package is in, which decides
	// what of other packages' lib it may use: empty for a program built
	// from a single file, which has none.
	Module string
	// Files are the package's files, in the byte order of their names.
	Files []*File

	// Dir is the slash-separated path of the directory the loader read
	// the package from, and CFiles the path of every C file in it, a file
	// whose name ends in .c, in the byte order of their names: the files
	// that define the package's extern funcs, which the driver compiles
	// and links with the program. Both are left out of the JSON dump,
	// which shows the Tack source only.
	Dir    string   `json:"-"`
	CFiles []string `json:"-"`
}

// Decls returns the package-level declarations of every file of the
// package, file by file in Files order and in source order within a file.
func (p *Package) Decls() []Decl {
	var decls []Decl
	for _, f := range p.Files {
		decls = append(decls, f.Decls...)
	}
	return decls
}

// Program is everything one compilation compiles: its packages, and once
// the type checker has run, the table of the types they use.
type Program struct {
	// Types describes the types the program's nodes refer to. The type
	// checker fills it in, and records it on every file too; it is nil
	// before then, and left out of the JSON dump, which is taken before
	// checking.
	Types *types.Table `json:"-"`

	// Packages are the program's packages. The package being built, the
	// main package, is the last.
	Packages []*Package
}

// Main returns the package being built: the last one. It returns nil for a
// program with no packages.
func (p *Program) Main() *Package {
	if len(p.Packages) == 0 {
		return nil
	}
	return p.Packages[len(p.Packages)-1]
}

// Decls returns the package-level declarations of every package of the
// program, package by package in Packages order, as Package.Decls orders
// each.
func (p *Program) Decls() []Decl {
	var decls []Decl
	for _, pkg := range p.Packages {
		decls = append(decls, pkg.Decls()...)
	}
	return decls
}

// Files returns every file of the program, package by package in Packages
// order.
func (p *Program) Files() []*File {
	var files []*File
	for _, pkg := range p.Packages {
		files = append(files, pkg.Files...)
	}
	return files
}

// SingleFile returns the program a single file makes on its own: one
// package, with path main, holding only file, and the file's types, if it
// has been checked.
func SingleFile(file *File) *Program {
	return &Program{
		Types:    file.Types,
		Packages: []*Package{{Name: file.Package, Path: "main", Files: []*File{file}}},
	}
}
