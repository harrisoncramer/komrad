package ui

import (
	"errors"
	"slices"

	"komrad/config"
	"komrad/internal/ui/form"

	"charm.land/huh/v2"
)

const (
	deleteFormKey  = "delete"
	chooseWorktree = "worktree"
	chooseRecords  = "records"
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

	return form.TitledOf(keys, title,
		huh.NewMultiSelect[string]().Key(deleteFormKey).Options(options...).Value(&v.chosen),
		confirm,
	)
}
