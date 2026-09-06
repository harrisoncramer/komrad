package git

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Edit is one run of adjacent lines a working tree has written over the commit it is checked out
// at, held in that commit's own line numbers so each run can be proposed on its own.
type Edit struct {
	Path string
	// Start and End name the run in the checked-out commit's copy, and End is one below Start for
	// a run that puts lines in without replacing any.
	Start int
	End   int
	Lines []string
}

// Edits is what a working tree has written since it was checked out, one entry per run of adjacent
// lines, which is what lets two changes to one file be proposed separately.
func Edits(ctx context.Context, dir string) ([]Edit, error) {
	raw, err := RunBytes(ctx, dir, "-c", "core.quotepath=false", "--no-pager", "diff",
		"--no-ext-diff", "--unified=0", "HEAD", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read what was written in %q: %w", dir, err)
	}

	files, _, err := gitdiff.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse what was written in %q: %w", dir, err)
	}

	var edits []Edit
	for _, file := range files {
		if file.IsBinary || file.NewName == "" {
			continue
		}

		for _, fragment := range file.TextFragments {
			edits = append(edits, editOf(file.NewName, fragment))
		}
	}

	return edits, nil
}

// editOf reads one fragment as a run, spelling an insertion as an empty range starting after the
// line git hung it off, which is the same shape a replacement has with nothing to replace.
func editOf(path string, fragment *gitdiff.TextFragment) Edit {
	start := int(fragment.OldPosition)
	if fragment.OldLines > 0 {
		return Edit{
			Path:  path,
			Start: start,
			End:   start + int(fragment.OldLines) - 1,
			Lines: addedLines(fragment),
		}
	}

	return Edit{
		Path:  path,
		Start: start + 1,
		End:   start,
		Lines: addedLines(fragment),
	}
}

func addedLines(fragment *gitdiff.TextFragment) []string {
	var lines []string
	for _, line := range fragment.Lines {
		if line.Op == gitdiff.OpAdd {
			lines = append(lines, strings.TrimSuffix(line.Line, "\n"))
		}
	}

	return lines
}
