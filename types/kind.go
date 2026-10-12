package types

// Kind is a Tack type. The language has no composite types yet, so a kind is
// the whole of what a type is.
type Kind int

const (
	// Invalid is the zero value, carried by an expression that has not been
	// checked or whose type could not be determined.
	Invalid Kind = iota
	// Int is a signed 64-bit integer.
	Int
	// String is a string of bytes.
	String
	// Bool is true or false; only comparisons produce one today.
	Bool
)

var kindNames = map[Kind]string{
	Invalid: "invalid",
	Int:     "int",
	String:  "string",
	Bool:    "bool",
}

// Lookup returns the kind a type name denotes. Invalid has no name a program
// can write, so it is never returned as a match.
func Lookup(name string) (Kind, bool) {
	for kind, kindName := range kindNames {
		if kind != Invalid && kindName == name {
			return kind, true
		}
	}
	return Invalid, false
}

// String returns the kind's name as a program writes it.
func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "invalid"
}
