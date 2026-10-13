package ast

// Visibility says which packages may use a package-level declaration or a
// struct field by name: its own package only, every package of its module,
// or every package. The levels are ordered, Private below Lib below Pub, so
// that the visibility a field has in effect, capped by its struct's, is the
// smaller of the two.
type Visibility uint8

// The visibilities, from least to most visible.
const (
	// Private is the default: usable only in the declaring package.
	Private Visibility = iota
	// Lib, written lib, is usable in every package of the declaring
	// package's module.
	Lib
	// Pub, written pub, is usable in every package.
	Pub
)

// String returns the keyword that gives v, or "private" for Private, which
// has none.
func (v Visibility) String() string {
	switch v {
	case Lib:
		return "lib"
	case Pub:
		return "pub"
	default:
		return "private"
	}
}

// Capped returns v limited to limit: the smaller of the two.
func (v Visibility) Capped(limit Visibility) Visibility {
	return min(v, limit)
}

// MarshalText writes v as String does, so a syntax tree dump shows the
// keyword.
func (v Visibility) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

// Visible reports whether a name of package owner with visibility vis may
// be used in package user: always in its own package, and in another when
// it is pub, or lib and the two packages are in one module.
func Visible(vis Visibility, owner, user *Package) bool {
	switch {
	case owner == user || vis == Pub:
		return true
	case vis == Lib:
		return owner.Module == user.Module
	default:
		return false
	}
}
