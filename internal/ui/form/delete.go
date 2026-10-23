package form

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	"charm.land/huh/v2"
)

// uncommittedFilesShown is how many uncommitted files a delete confirm names before it counts the rest.
const uncommittedFilesShown = 10

const (
	deleteKey      = "delete"
	chooseWorktree = "worktree"
	chooseRecords  = "records"
)

var errNothingChosen = errors.New("check what to delete, or go back")

// Delete is a delete confirm that says what the worktrees the delete takes still hold.
type Delete struct {
	Title           string
	Warning         string
	Dirs            []string
	StartsConfirmed bool
	// Values asks what the delete takes, unset where the delete takes directories alone.
	Values *DeleteValues
}

// Open reads each worktree afresh and puts the confirm on screen, sending what write asks for
// once it is answered yes.
func (c Delete) Open(in screen.Inputs, write func(Inputs) screen.Request) screen.Request {
	confirmed := c.StartsConfirmed

	var requests screen.Batch
	for _, dir := range c.Dirs {
		if !in.Inspections[dir].IsSettled() {
			requests = append(requests, screen.Inspect{Dir: dir})
		}
	}

	return append(requests, Open{Form: Form{
		Build: func(in Inputs) (*huh.Form, []string) {
			confirm := confirmField(&confirmed, strings.TrimSpace(c.Warning+describeStanding(in.Inspections, c.Dirs)), "Delete")
			if c.Values == nil {
				return Of(config.Fixed(), confirm.Title(c.Title))
			}

			return c.Values.form(config.Fixed(), c.Title, confirm)
		},
		Submit: func(submitted Inputs) screen.Request {
			if !confirmed {
				return nil
			}
			if c.Values != nil {
				if err := c.Values.check(); err != nil {
					return Refuse{Err: err, Field: deleteKey}
				}
			}

			return write(submitted)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
		Worktrees:       c.Dirs,
	}})
}

// describeStanding says what is uncommitted and unpushed in the worktrees, as git last answered for each.
func describeStanding(inspections map[string]panel.WorktreeInspection, dirs []string) string {
	var files, refusals []string
	var commits, uncounted, read, reading, settled int
	for _, dir := range dirs {
		inspection := inspections[dir]
		switch {
		case !inspection.Answered || inspection.Pending:
			reading++
		case inspection.IsSettled():
			settled++
		case inspection.Err != nil:
			refusals = append(refusals, errui.GetReason(inspection.Err))
		case !inspection.Item.Readable:
			refusals = append(refusals, "git cannot read it")
		default:
			read++
			files = slices.Concat(files, inspection.Modified, inspection.Untracked)
			if unpushed, ok := inspection.UnpushedCommits(); ok {
				commits += unpushed
			} else {
				uncounted++
			}
		}
	}

	worktrees := len(dirs) - settled
	which := func(n int) string {
		if worktrees == 1 {
			return "the worktree"
		}

		return label.Count(n, "worktree", "worktrees")
	}

	var b strings.Builder
	counts := describeUncommitted(len(files), commits, uncounted == 0)
	switch {
	case read == 0:
	case worktrees == 1:
		fmt.Fprintf(&b, " The worktree has %s.", counts)
	case read == worktrees:
		fmt.Fprintf(&b, " The worktrees have %s.", counts)
	default:
		fmt.Fprintf(&b, " The %s %s.", label.Count(read, "worktree Komrad read has", "worktrees Komrad read have"), counts)
	}
	switch {
	case len(files) == 0:
	case worktrees == 1:
		b.WriteString(" Deleting the worktree loses the uncommitted files.")
	default:
		b.WriteString(" Deleting the worktrees loses the uncommitted files.")
	}
	if uncounted > 0 {
		fmt.Fprintf(&b, " Without a base, Komrad cannot count the unpushed commits of %s.", which(uncounted))
	}
	if reading > 0 {
		fmt.Fprintf(&b, " Komrad is still reading %s, so it cannot yet say what is uncommitted or unpushed there.", which(reading))
	}
	if len(refusals) > 0 {
		why := ""
		if worktrees == 1 {
			why = " (" + refusals[0] + ")"
		}
		fmt.Fprintf(&b, " Komrad could not read %s%s, so it cannot say what is uncommitted or unpushed there.", which(len(refusals)), why)
	}
	if read == 1 {
		writeUncommittedFiles(&b, files)
	}

	return b.String()
}

// describeUncommitted says how many uncommitted files and unpushed commits there are, as far as they were counted.
func describeUncommitted(files, commits int, counted bool) string {
	var parts []string
	if files > 0 {
		parts = append(parts, label.Count(files, "uncommitted file", "uncommitted files"))
	}
	if commits > 0 {
		parts = append(parts, label.Count(commits, "unpushed commit", "unpushed commits"))
	}

	switch {
	case len(parts) > 0:
		return strings.Join(parts, " and ")
	case counted:
		return "nothing uncommitted or unpushed"
	}

	return "nothing uncommitted"
}

// writeUncommittedFiles lists the uncommitted files one to a line, counting those past uncommittedFilesShown.
func writeUncommittedFiles(b *strings.Builder, files []string) {
	for _, file := range files[:min(len(files), uncommittedFilesShown)] {
		b.WriteString("\n  " + file)
	}
	if hidden := len(files) - uncommittedFilesShown; hidden > 0 {
		fmt.Fprintf(b, "\n  and %d more", hidden)
	}
}

// DeleteValues is what a delete form offers to take, and what the person has checked.
type DeleteValues struct {
	// offersWorktree is whether the delete has a worktree komrad cut to take.
	offersWorktree bool
	chosen         []string
}

// NewDeleteValues starts on the worktree alone, or on the records where there is no worktree to take.
func NewDeleteValues(offersWorktree bool) *DeleteValues {
	if offersWorktree {
		return &DeleteValues{offersWorktree: true, chosen: []string{chooseWorktree}}
	}

	return &DeleteValues{chosen: []string{chooseRecords}}
}

// DeletesRecords reports whether the person checked the records.
func (v *DeleteValues) DeletesRecords() bool {
	return slices.Contains(v.chosen, chooseRecords)
}

// check refuses a delete that takes nothing.
func (v *DeleteValues) check() error {
	if len(v.chosen) == 0 {
		return errNothingChosen
	}

	return nil
}

// form asks what the delete takes above the confirm that runs it.
func (v *DeleteValues) form(keys config.FixedKeys, title string, confirm *huh.Confirm) (*huh.Form, []string) {
	var options []huh.Option[string]
	if v.offersWorktree {
		options = append(options, huh.NewOption("worktree", chooseWorktree))
	}
	options = append(options, huh.NewOption("records", chooseRecords))

	return titledOf(keys, title,
		huh.NewMultiSelect[string]().Key(deleteKey).Options(options...).Value(&v.chosen),
		confirm,
	)
}
