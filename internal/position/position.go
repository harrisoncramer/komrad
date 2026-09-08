package position

// Range is a span of lines numbered from one, holding its start and not its end.
type Range struct {
	Start int
	End   int
}

// IsEmpty reports whether a range covers no lines.
func (r Range) IsEmpty() bool {
	return r.Start == r.End
}

// IsValid reports whether a range names lines a file could hold.
func (r Range) IsValid() bool {
	return r.Start >= 1 && r.End >= r.Start
}

// doesOverlap reports whether two ranges share a line.
func (r Range) doesOverlap(other Range) bool {
	if r.IsEmpty() || other.IsEmpty() {
		return false
	}

	return r.Start < other.End && other.Start < r.End
}

// Position is a file in one commit and the lines of it a caller cares about.
type Position struct {
	Path  string
	Lines Range
}

// IsFileLevel reports whether a position names a file and none of its lines.
func (p Position) IsFileLevel() bool {
	return p.Lines == Range{}
}

// FileMapping is where one file went, with no new path for a file the later commit does not hold.
type FileMapping struct {
	OldPath string
	NewPath string
}

// IsDeleted reports whether the later commit holds no file for this mapping.
func (f FileMapping) IsDeleted() bool {
	return f.NewPath == ""
}

// RangeMapping pairs a region of the old file with the lines standing in its place.
type RangeMapping struct {
	Old Range
	New Range
}

// Mapping is where one file went and which of its regions were rewritten.
type Mapping struct {
	File   FileMapping
	Ranges []RangeMapping
}

// Policy is what to do with a position whose lines the later commit rewrote.
type Policy uint8

const (
	// OmitOnConflict gives up on a position the later commit wrote over.
	OmitOnConflict Policy = iota

	// FileOnConflict keeps the file of a position the later commit wrote over and lets its lines go.
	FileOnConflict
)

// Outcome is what became of one position.
type Outcome uint8

const (
	// Placed is a position the later commit holds lines for.
	Placed Outcome = iota

	// FileOnly is a position whose lines are gone and whose file is not.
	FileOnly

	// Dropped is a position with nothing left to stand on.
	Dropped
)

// Result is where one position ended up, named by the index it arrived at.
type Result struct {
	Index    int
	Position Position
	Outcome  Outcome
}
