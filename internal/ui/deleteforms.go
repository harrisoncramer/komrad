package ui

import (
	"errors"
	"slices"

	"komrad/config"

	"charm.land/huh/v2"
)

const (
	deleteFormKey  = "delete"
	chooseWorktree = "worktree"
	chooseRecords  = "records"
)

const (
	recordsWarning             = "Deleting the records deletes the transcripts, documents, mail, notes and reviews for good."
	recordsWithWorktreeWarning = "Deleting the records deletes the worktree with them, and the transcripts, documents, mail, notes and reviews for good."
)

var errNothingChosen = errors.New("check what to delete, or go back")

// deleteFormValues is what a delete form offers to take, and what the person has checked.
type deleteFormValues struct {
	// offersWorktree is whether the delete has a worktree komrad cut to take.
	offersWorktree bool
	chosen         []string
}

// newDeleteFormValues starts on the worktree alone, or on the records where there is no worktree to take.
func newDeleteFormValues(offersWorktree bool) *deleteFormValues {
	if offersWorktree {
		return &deleteFormValues{offersWorktree: true, chosen: []string{chooseWorktree}}
	}

	return &deleteFormValues{chosen: []string{chooseRecords}}
}

// deletesRecords reports whether the person checked the records.
func (v *deleteFormValues) deletesRecords() bool {
	return slices.Contains(v.chosen, chooseRecords)
}

// warning says what deleting the records takes.
func (v *deleteFormValues) warning() string {
	if v.offersWorktree {
		return recordsWithWorktreeWarning
	}

	return recordsWarning
}

// check refuses a delete that takes nothing.
func (v *deleteFormValues) check() error {
	if len(v.chosen) == 0 {
		return errNothingChosen
	}

	return nil
}

// form asks what the delete takes above the confirm that runs it.
func (v *deleteFormValues) form(keys config.FixedKeys, title string, confirm *huh.Confirm) (*huh.Form, []string) {
	var options []huh.Option[string]
	if v.offersWorktree {
		options = append(options, huh.NewOption("worktree", chooseWorktree))
	}
	options = append(options, huh.NewOption("records", chooseRecords))

	return titledFormOf(keys, title,
		huh.NewMultiSelect[string]().Key(deleteFormKey).Options(options...).Value(&v.chosen),
		confirm,
	)
}
