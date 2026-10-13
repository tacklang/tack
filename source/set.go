package source

// Files finds the File a span points into, by the FileID the span carries.
// Diagnostics are rendered, and the type checker quotes source, through it.
type Files interface {
	// Lookup returns the File with ID id, or nil if there is none.
	Lookup(id FileID) *File
}

// Lookup returns f whatever id is. A File on its own stands for the whole
// input, as when a single file is compiled, so every span is taken to point
// into it.
func (f *File) Lookup(FileID) *File { return f }

// Set is the files one compilation reads, found by ID. The zero value is an
// empty Set ready to use.
type Set struct {
	byID  map[FileID]*File
	files []*File
}

// NewSet returns a Set holding files, in that order.
func NewSet(files ...*File) *Set {
	s := &Set{}
	for _, f := range files {
		s.Add(f)
	}
	return s
}

// Add adds f to the Set. A file with the same ID as one already added
// replaces it in lookups.
func (s *Set) Add(f *File) {
	if s.byID == nil {
		s.byID = make(map[FileID]*File)
	}
	s.byID[f.ID] = f
	s.files = append(s.files, f)
}

// Lookup returns the file with ID id, or nil if the Set has none.
func (s *Set) Lookup(id FileID) *File {
	if s == nil {
		return nil
	}
	return s.byID[id]
}

// Files returns the files in the order they were added. The slice is
// shared, not copied.
func (s *Set) Files() []*File {
	if s == nil {
		return nil
	}
	return s.files
}
