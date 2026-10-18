package diag

// Severity says how serious a diagnostic is.
type Severity int

const (
	// Note adds context to another diagnostic.
	Note Severity = iota
	// Warning reports something suspicious that does not stop compilation.
	Warning
	// Error reports a problem that stops compilation.
	Error
)

// ErrorCount returns how many of diags are errors: the ones that stop
// compilation, which warnings and notes do not.
func ErrorCount(diags []Diagnostic) int {
	n := 0
	for _, d := range diags {
		if d.Severity == Error {
			n++
		}
	}
	return n
}

// HasErrors reports whether any of diags is an error.
func HasErrors(diags []Diagnostic) bool {
	return ErrorCount(diags) > 0
}

// String returns the severity's lowercase name as diagnostics print it.
func (l Severity) String() string {
	switch l {
	case Note:
		return "note"
	case Warning:
		return "warning"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}
