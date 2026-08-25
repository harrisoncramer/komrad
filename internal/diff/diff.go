// Package diff loads the change an agent has made in its worktree and parses it into
// lines that carry their own numbers, so a cursor over the diff can be turned back into a
// place in a file.
package diff

// LineKind is what one line of a hunk does.
type LineKind uint8

const (
	// Context is a line both sides share.
	Context LineKind = iota
	// Addition is a line only the new side has.
	Addition
	// Deletion is a line only the old side has.
	Deletion
)

// Line is one line of a hunk. A number is zero on the side the line does not appear on.
type Line struct {
	Kind LineKind

	Text string

	OldNumber int
	NewNumber int
}

// Hunk is one run of changed lines with the context around it.
type Hunk struct {
	Header string

	Lines []Line
}

// File is one file's worth of change, along with both sides of its source. The sources are
// carried whole rather than line by line because the highlighter reads them that way: a
// language is lexed over a file, not over the fragments of it a patch happens to show.
type File struct {
	OldPath string
	NewPath string

	Metadata []string

	OldSource string
	NewSource string

	Hunks []Hunk
}

// Path is the name to show the file under, which is where it ended up unless it was deleted.
func (f File) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}

// Patch is everything an agent has changed, measured against one base commit.
type Patch struct {
	Root string

	Base   string
	Commit string

	Files []File
}

// IsEmpty reports whether the patch has nothing to show.
func (p Patch) IsEmpty() bool { return len(p.Files) == 0 }
