package ui

import (
	"errors"
	"slices"

	"komrad/internal/ui/paint"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

const (
	deleteBoxesKey = "delete"
	tickWorktree   = "worktree"
	tickRecords    = "records"
)

const recordsWarning = "Deleting the records deletes the transcripts, documents, mail, notes and reviews for good."

var (
	errNothingTicked          = errors.New("tick what to delete, or go back")
	errRecordsWithoutWorktree = errors.New("records will only be deleted with the worktree")
)

// deleteBoxes is what a delete form offers to take, and what the person has ticked.
type deleteBoxes struct {
	// offersWorktree is whether the delete has a worktree komrad cut to take.
	offersWorktree bool
	ticked         []string
}

// newDeleteBoxes starts on the worktree alone, or on the records where there is no worktree to take.
func newDeleteBoxes(offersWorktree bool) *deleteBoxes {
	if offersWorktree {
		return &deleteBoxes{offersWorktree: true, ticked: []string{tickWorktree}}
	}

	return &deleteBoxes{ticked: []string{tickRecords}}
}

// deletesRecords reports whether the person ticked the records.
func (b *deleteBoxes) deletesRecords() bool {
	return slices.Contains(b.ticked, tickRecords)
}

// check refuses a delete that takes nothing, or the records without the worktree they sit in.
func (b *deleteBoxes) check() error {
	switch {
	case len(b.ticked) == 0:
		return errNothingTicked
	case b.offersWorktree && b.deletesRecords() && !slices.Contains(b.ticked, tickWorktree):
		return errRecordsWithoutWorktree
	}

	return nil
}

// form asks what the delete takes above the confirm that runs it.
func (b *deleteBoxes) form(params confirmFormStartingOnParams) *huh.Form {
	var options []huh.Option[string]
	if b.offersWorktree {
		options = append(options, huh.NewOption("worktree", tickWorktree))
	}
	options = append(options, huh.NewOption("records", tickRecords))

	return huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Key(deleteBoxesKey).
				Options(options...).
				Value(&b.ticked),
			huh.NewConfirm().
				Key("confirm").
				Description(params.description).
				Affirmative(params.affirmative).
				Negative("Cancel").
				WithButtonAlignment(lipgloss.Left).
				Value(params.answer),
		).Title(params.title),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(params.keys)).WithTheme(paint.Panel.FormTheme())
}
