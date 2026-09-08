// Package position moves a place in one commit to the place it has become in another, which is
// how a comment written against an old snapshot is shown against a newer one.
package position

// Range is a span of lines, numbered from one, holding its start and not its end, so that a span
// of no lines is a point between two of them rather than a line of its own.
type Range struct {
	Start int
	End   int
}

// IsEmpty reports whether a range covers no lines, which is the shape an insertion point takes.
func (r Range) IsEmpty() bool {
	return r.Start == r.End
}

// IsValid reports whether a range names lines a file could hold, since a caller reading a stored
// row can hand over anything at all.
func (r Range) IsValid() bool {
	return r.Start >= 1 && r.End >= r.Start
}

// doesOverlap reports whether two ranges share a line, which two spans meeting end to start do
// not, since a range holds its start and not its end.
func (r Range) doesOverlap(other Range) bool {
	if r.IsEmpty() || other.IsEmpty() {
		return false
	}

	return r.Start < other.End && other.Start < r.End
}

// Position is a place in one commit: a file, and the lines of it a caller cares about, which is
// no lines at all for something said about the file as a whole.
type Position struct {
	Path  string
	Lines Range
}

// IsFileLevel reports whether a position names a file rather than any of its lines, which is the
// zero range and not merely an empty one, since an empty range at a real line is the point
// between two lines that an insertion sits at.
func (p Position) IsFileLevel() bool {
	return p.Lines == Range{}
}

// FileMapping is what became of one file between the two commits, with no new path at all for a
// file the later commit does not hold.
type FileMapping struct {
	OldPath string
	NewPath string
}

// IsDeleted reports whether the later commit holds no file for this mapping.
func (f FileMapping) IsDeleted() bool {
	return f.NewPath == ""
}

// RangeMapping is one region of a file the later commit rewrote, pairing the lines it held before
// with the lines standing in their place afterwards.
type RangeMapping struct {
	Old Range
	New Range
}

// Mapping is everything that happened to one file, which is where it went and which of its
// regions were rewritten on the way.
type Mapping struct {
	File   FileMapping
	Ranges []RangeMapping
}

// Policy is what to do with a position whose lines the later commit rewrote, which is a question
// the caller answers rather than this package, since a comment and a fold want opposite things.
type Policy uint8

const (
	// OmitOnConflict gives up on a position the later commit wrote over.
	OmitOnConflict Policy = iota

	// FileOnConflict keeps the file of a position the later commit wrote over and lets its lines
	// go, so that what was said about those lines is still said about that file.
	FileOnConflict
)

// Outcome is what became of one position, which the caller needs in order to say honestly where
// what it is showing came from.
type Outcome uint8

const (
	// Placed is a position the later commit holds lines for.
	Placed Outcome = iota

	// FileOnly is a position whose lines are gone and whose file is not, kept under
	// FileOnConflict and never returned under OmitOnConflict.
	FileOnly

	// Dropped is a position with nothing left to stand on, which is a file the later commit does
	// not hold, a range it wrote over under OmitOnConflict, or a range no file could have held.
	Dropped
)

// Result is where one of the positions handed in ended up, named by the index it arrived at so a
// caller can match it back even when a copied file turns one position into two.
type Result struct {
	Index    int
	Position Position
	Outcome  Outcome
}
