// Package diff loads the change an agent has made in its worktree and parses it into
// lines that carry their own numbers, so a cursor over the diff can be turned back into a
// place in a file.
package diff

import (
	"crypto/sha256"
	"encoding/hex"
)

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

// Line is one line of a hunk, with a zero number on the side it does not appear on.
type Line struct {
	Kind      LineKind
	Text      string
	OldNumber int
	NewNumber int
}

// Hunk is one run of changed lines with the context around it.
type Hunk struct {
	Header string
	Lines  []Line
	// OldStart and NewStart are the first line each side of the hunk covers, which is what the
	// header states and what the gap above it is numbered from.
	OldStart int
	NewStart int
}

// File is one file's worth of change with both sides of its source carried whole.
type File struct {
	OldPath   string
	NewPath   string
	Metadata  []string
	OldSource string
	NewSource string
	Hunks     []Hunk
	// Content identifies what a file holds when the patch draws none of it, as for a binary, and
	// is empty for a file whose hunks carry its change.
	Content string
}

// Path is the name to show the file under, which is where it ended up unless it was deleted.
func (f File) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}

// Patch is everything one round holds, measured against the base the round was taken against.
type Patch struct {
	Root        string
	BaseSHA     string
	SnapshotSHA string
	Files       []File
}

// Stat is the size of a patch: how many files it touches, and how many lines it puts in and
// takes out.
type Stat struct {
	Files   int
	Added   int
	Removed int
}

// Stat counts the patch up.
func (p Patch) Stat() Stat {
	stat := Stat{Files: len(p.Files)}
	for _, file := range p.Files {
		counted := file.Stat()
		stat.Added += counted.Added
		stat.Removed += counted.Removed
	}

	return stat
}

// Fingerprint is a hex-encoded SHA-256 of the file's metadata, hunk headers and hunk lines.
func (f File) Fingerprint() string {
	h := sha256.New()
	for _, m := range f.Metadata {
		h.Write([]byte(m))
		h.Write([]byte{'\n'})
	}
	for _, hunk := range f.Hunks {
		h.Write([]byte(hunk.Header))
		h.Write([]byte{'\n'})
		for _, line := range hunk.Lines {
			h.Write([]byte{kindByte(line.Kind)})
			h.Write([]byte(line.Text))
			h.Write([]byte{'\n'})
		}
	}
	if f.Content != "" {
		h.Write([]byte(f.Content))
		h.Write([]byte{'\n'})
	}

	return hex.EncodeToString(h.Sum(nil))
}

func kindByte(k LineKind) byte {
	switch k {
	case Addition:
		return '+'
	case Deletion:
		return '-'
	case Context:
		return ' '
	}

	return 0
}

// Stat is the size of one file's change.
func (f File) Stat() Stat {
	stat := Stat{Files: 1}
	for _, hunk := range f.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case Addition:
				stat.Added++
			case Deletion:
				stat.Removed++
			case Context:
			}
		}
	}

	return stat
}
