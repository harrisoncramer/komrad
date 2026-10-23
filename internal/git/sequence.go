package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	taskv1 "komrad/proto/gen/task/v1"
)

// sequenceMarkers are the paths git keeps while a sequence owns the index, in the order they are read.
var sequenceMarkers = []struct {
	path     string
	sequence taskv1.Sequence
}{
	{path: "rebase-merge", sequence: taskv1.Sequence_SEQUENCE_REBASE},
	{path: "rebase-apply", sequence: taskv1.Sequence_SEQUENCE_REBASE},
	{path: "MERGE_HEAD", sequence: taskv1.Sequence_SEQUENCE_MERGE},
	{path: "CHERRY_PICK_HEAD", sequence: taskv1.Sequence_SEQUENCE_CHERRY_PICK},
	{path: "REVERT_HEAD", sequence: taskv1.Sequence_SEQUENCE_REVERT},
	{path: "sequencer", sequence: taskv1.Sequence_SEQUENCE_CHERRY_PICK},
}

// IsInProgress reports whether the sequence is anything at all.
func IsInProgress(sequence taskv1.Sequence) bool {
	return sequence != taskv1.Sequence_SEQUENCE_NONE && sequence != taskv1.Sequence_SEQUENCE_UNSPECIFIED
}

// GetSequenceCommand is the git command that runs the sequence, empty when nothing is in progress.
func GetSequenceCommand(sequence taskv1.Sequence) string {
	switch sequence {
	case taskv1.Sequence_SEQUENCE_REBASE:
		return "rebase"
	case taskv1.Sequence_SEQUENCE_AM:
		return "am"
	case taskv1.Sequence_SEQUENCE_MERGE:
		return "merge"
	case taskv1.Sequence_SEQUENCE_CHERRY_PICK:
		return "cherry-pick"
	case taskv1.Sequence_SEQUENCE_REVERT:
		return "revert"
	case taskv1.Sequence_SEQUENCE_UNSPECIFIED, taskv1.Sequence_SEQUENCE_NONE:
	}

	return ""
}

// ReadSequence is what git has part-way through in a checkout, read without touching the index.
func (c *Client) ReadSequence(ctx context.Context, dir string) (taskv1.Sequence, error) {
	return readCheckoutSequence(ctx, dir)
}

// readCheckoutSequence is what git has part-way through in the checkout at dir.
func readCheckoutSequence(ctx context.Context, dir string) (taskv1.Sequence, error) {
	gitDir, err := readGitDir(ctx, dir)
	if err != nil {
		return taskv1.Sequence_SEQUENCE_NONE, err
	}

	return readSequence(gitDir), nil
}

func readSequence(gitDir string) taskv1.Sequence {
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

	return taskv1.Sequence_SEQUENCE_NONE
}

// readApplySequence tells an am from a rebase run on the apply backend, which share a directory.
func readApplySequence(gitDir string) taskv1.Sequence {
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-apply", "applying")); err == nil {
		return taskv1.Sequence_SEQUENCE_AM
	}

	return taskv1.Sequence_SEQUENCE_REBASE
}

// readSequencerSequence is the kind of a multi-commit pick resting between two of its commits,
// read off the first line still to do.
func readSequencerSequence(gitDir string) taskv1.Sequence {
	todo, err := os.ReadFile(filepath.Join(gitDir, "sequencer", "todo"))
	if err != nil {
		return taskv1.Sequence_SEQUENCE_CHERRY_PICK
	}

	for line := range strings.Lines(string(todo)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "revert ") {
			return taskv1.Sequence_SEQUENCE_REVERT
		}

		return taskv1.Sequence_SEQUENCE_CHERRY_PICK
	}

	return taskv1.Sequence_SEQUENCE_CHERRY_PICK
}
