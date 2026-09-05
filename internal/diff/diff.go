// Package diff loads the change an agent has made in its worktree and parses it into
// lines that carry their own numbers, so a cursor over the diff can be turned back into a
// place in a file.
package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
}

// File is one file's worth of change with both sides of its source carried whole, since the
// highlighter lexes a language over a file rather than over the fragments a patch shows.
type File struct {
	OldPath   string
	NewPath   string
	Metadata  []string
	OldSource string
	NewSource string
	Hunks     []Hunk
	// Content identifies what a file holds when the patch draws none of it, as for a binary,
	// and is empty for a file whose hunks carry its change, since hashing source the reader was
	// never shown would drop a mark over a change they could not have missed.
	Content string
}

// Fingerprint is a hash of what the diff draws for one file, which is what a mark of having
// read that file is held against: the change made to it, the headers its hunks fall under, and
// every line of them, and the content of a file it draws nothing of, but not the source outside
// the hunks of one it does, since a change no hunk shows is a change the reader was never shown.
func (f File) Fingerprint() string {
	var drawn strings.Builder

	fmt.Fprintf(&drawn, "%s\x00%s\x00%s\x00", f.OldPath, f.NewPath, f.Content)
	for _, metadata := range f.Metadata {
		fmt.Fprintf(&drawn, "%s\x00", metadata)
	}

	for _, hunk := range f.Hunks {
		fmt.Fprintf(&drawn, "%s\x00", hunk.Header)
		for _, line := range hunk.Lines {
			fmt.Fprintf(&drawn, "%d%s\x00", line.Kind, line.Text)
		}
	}

	sum := sha256.Sum256([]byte(drawn.String()))

	return hex.EncodeToString(sum[:])
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

// IsEmpty reports whether the patch has nothing to show.
func (p Patch) IsEmpty() bool { return len(p.Files) == 0 }

// Fingerprint is what says a patch read again in the background differs from the one on screen,
// built out of the per-file hashes so it holds to the same rule they do: a change the reader was
// never shown is not one the patch has moved for.
func (p Patch) Fingerprint() string {
	var drawn strings.Builder

	for _, file := range p.Files {
		fmt.Fprintf(&drawn, "%s\x00", file.Fingerprint())
	}

	sum := sha256.Sum256([]byte(drawn.String()))

	return hex.EncodeToString(sum[:])
}

// Stat is the size of a patch in the terms a reviewer reads first: how many files it touches,
// and how many lines it puts in and takes out.
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

// Stat is the size of one file's change, which is what a collapsed file says about itself.
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
