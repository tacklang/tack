// Package modfile parses tack.mod, the root file that marks a module's root
// directory and names the module.
//
// The loader finds a tack.mod by walking up from the directory it is asked
// to build, reads it into a source.File, and passes it to Parse, which
// returns the File it declares or diagnostics with spans in it. The loader
// renders those like any other, which is why Parse records the file's line
// starts, as the lexer does for a Tack file.
//
// # Format
//
// tack.mod is line-oriented, as Go's go.mod is. A line ends at a newline,
// \n or \r\n. // starts a comment that runs to the end of its line,
// wherever it is, even inside a word, so module a//b names the module a. A line
// is split into words at spaces and tabs; a line with none is skipped. The
// first word is a directive, and the only directive today is module,
// followed by exactly one word, the module's name:
//
//	// tack.mod
//	module calc
//
// A file must declare its module exactly once. Any other directive, a
// module line without a name or with more than one, a second module line,
// and a name CheckModule rejects are each reported, at the word they are
// about, and a file with no module line is reported at its start. Parse
// reports every problem it finds, and returns a File only when there is
// none.
//
// # Module names
//
// A module name is one or more parts joined by slashes. A part is lowercase
// ASCII letters, digits, -, . and _, and neither starts nor ends with . or
// -. std is a name like any other: the standard library's own tack.mod says
// module std, and a checkout of it is checked as that module. There
// is no length limit. A name with a byte that is not printable ASCII is
// quoted in a diagnostic, so the byte can be seen.
package modfile
