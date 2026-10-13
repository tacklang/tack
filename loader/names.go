package loader

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/types"
)

// Names a package may not have, beyond keywords and builtins: builtin,
// which src/std/builtin documents the builtins under, and std, the
// standard library's module.
var reservedPackages = map[string]bool{"builtin": true, "std": true}

// checkPackageName returns why name cannot be a package's name, or "" if
// it can. A package name is lowercase ASCII letters and digits, starting
// with a letter, and is not a keyword, a builtin type's or function's
// name, builtin or std. main is a valid name; where it may be used is the
// caller's to check.
func checkPackageName(name string) string {
	var reason string
	_, isKeyword := lexer.LookupKeyword(name)
	_, isType := types.Lookup(name)
	_, isFunc := builtin.Lookup(name)
	switch {
	case !lowerAlnum(name):
		reason = "use lowercase letters and digits"
	case name[0] < 'a' || name[0] > 'z':
		reason = "start with a letter"
	case isKeyword:
		reason = "it is a keyword"
	case isType:
		reason = "it is a builtin type"
	case isFunc:
		reason = "it is a builtin function"
	case reservedPackages[name]:
		reason = "it is reserved"
	default:
		return ""
	}
	return fmt.Sprintf("%s is not a valid package name: %s", show(name), reason)
}

// lowerAlnum reports whether name is one or more lowercase ASCII letters
// and digits.
func lowerAlnum(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// checkImportPath returns why p cannot be an import path in the module
// named mod, or "" if it can. An import path starts with a module's name,
// never with . or /, and its parts between slashes are neither empty nor
// . or ..; whether it names a package is checked when it is looked up.
func checkImportPath(p, mod string) string {
	switch {
	case p == "":
		return "import path is empty"
	case strings.HasPrefix(p, "/"):
		return fmt.Sprintf("import path must start with a module name, such as %s; "+
			"absolute paths are not allowed", suggestPath(p, mod))
	case p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../"):
		return fmt.Sprintf("import path must start with a module name, such as %s; "+
			"relative paths are not allowed", suggestPath(p, mod))
	}
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "":
			return fmt.Sprintf("invalid import path %s: a part between slashes is empty", show(p))
		case ".", "..":
			return fmt.Sprintf("invalid import path %s: a part cannot be . or ..", show(p))
		}
	}
	return ""
}

// suggestPath returns the import path a relative or absolute path p
// probably means: its last part in the module named mod.
func suggestPath(p, mod string) string {
	base := path.Base(p)
	if base == "." || base == ".." || base == "/" {
		return mod + "/<package>"
	}
	return mod + "/" + show(base)
}

// show returns s as a message writes it: as it is if every byte of it is
// printable ASCII, and Go-quoted otherwise, so an odd byte is visible.
func show(s string) string {
	for i := range len(s) {
		if s[i] < ' ' || s[i] > '~' {
			return strconv.Quote(s)
		}
	}
	return s
}
