package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Sequence is what git has part-way through in a checkout.
type Sequence string

const (
	SequenceNone       Sequence = "none"
	SequenceRebase     Sequence = "rebase"
	SequenceApply      Sequence = "am"
	SequenceMerge      Sequence = "merge"
	SequenceCherryPick Sequence = "cherry_pick"
	SequenceRevert     Sequence = "revert"
)

// sequenceMarkers are the paths git keeps while a sequence owns the index, in the order they are read.
var sequenceMarkers = []struct {
	path     string
	sequence Sequence
}{
	{path: "rebase-merge", sequence: SequenceRebase},
	{path: "rebase-apply", sequence: SequenceRebase},
	{path: "MERGE_HEAD", sequence: SequenceMerge},
	{path: "CHERRY_PICK_HEAD", sequence: SequenceCherryPick},
	{path: "REVERT_HEAD", sequence: SequenceRevert},
	{path: "sequencer", sequence: SequenceCherryPick},
}

// IsInProgress reports whether the sequence is anything at all.
func (s Sequence) IsInProgress() bool {
	return s != SequenceNone && s != ""
}

// GetCommand is the git command that runs the sequence, empty when nothing is in progress.
func (s Sequence) GetCommand() string {
	switch {
	case !s.IsInProgress():
		return ""
	case s == SequenceCherryPick:
		return "cherry-pick"
	}

	return string(s)
}

// ReadSequence is what git has part-way through in a checkout, read without touching the index.
func (c *Client) ReadSequence(ctx context.Context, dir string) (Sequence, error) {
	return readCheckoutSequence(ctx, dir)
}

// readCheckoutSequence is what git has part-way through in the checkout at dir.
func readCheckoutSequence(ctx context.Context, dir string) (Sequence, error) {
	gitDir, err := readGitDir(ctx, dir)
	if err != nil {
		return SequenceNone, err
	}

	return readSequence(gitDir), nil
}

func readSequence(gitDir string) Sequence {
	for _, marker := range sequenceMarkers {
		if _, err := os.Stat(filepath.Join(gitDir, marker.path)); err != nil {
			continue
		}

		switch marker.path {
		case "rebase-apply":
			return readApplySequence(gitDir)
		case "sequencer":
			return readSequencerSequence(gitDir)
		}

		return marker.sequence
	}

	return SequenceNone
}

// readApplySequence tells an am from a rebase run on the apply backend, which share a directory.
func readApplySequence(gitDir string) Sequence {
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-apply", "applying")); err == nil {
		return SequenceApply
	}

	return SequenceRebase
}

// readSequencerSequence is the kind of a multi-commit pick resting between two of its commits,
// read off the first line still to do.
func readSequencerSequence(gitDir string) Sequence {
	todo, err := os.ReadFile(filepath.Join(gitDir, "sequencer", "todo"))
	if err != nil {
		return SequenceCherryPick
	}

	for line := range strings.Lines(string(todo)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "revert ") {
			return SequenceRevert
		}

		return SequenceCherryPick
	}

	return SequenceCherryPick
}
