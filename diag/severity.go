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
