package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/form"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	"charm.land/huh/v2"
)

// uncommittedFilesShown is how many uncommitted files a delete confirm names before it counts the rest.
const uncommittedFilesShown = 10

// worktreeConfirm is a delete confirm that says what the worktrees the delete takes still hold.
type worktreeConfirm struct {
	title           string
	warning         string
	dirs            []string
	startsConfirmed bool
	// values asks what the delete takes, unset where the delete takes directories alone.
	values *deleteFormValues
}

// open reads each worktree afresh and puts the confirm on screen, sending what write asks for
// once it is answered yes.
func (c worktreeConfirm) open(in screen.Inputs, write func(form.Inputs) screen.Request) screen.Request {
	confirmed := c.startsConfirmed

	var requests screen.Batch
	for _, dir := range c.dirs {
		if !in.Inspections[dir].IsSettled() {
			requests = append(requests, screen.Inspect{Dir: dir})
		}
	}

	return append(requests, form.Open{Form: form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			confirm := form.ConfirmField(&confirmed, strings.TrimSpace(c.warning+describeStanding(in.Inspections, c.dirs)), "Delete")
			if c.values == nil {
				return form.Of(fixedKeys(), confirm.Title(c.title))
			}

			return c.values.form(fixedKeys(), c.title, confirm)
		},
		Submit: func(submitted form.Inputs) screen.Request {
			if !confirmed {
				return nil
			}
			if c.values != nil {
				if err := c.values.check(); err != nil {
					return form.Refuse{Err: err, Field: deleteFormKey}
				}
			}

			return write(submitted)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
		Worktrees:       c.dirs,
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

// deleteWorktreesConfirm asks before the remover takes the directories no task answers for.
func deleteWorktreesConfirm(deletion worktreeDeletion) worktreeConfirm {
	count := deletion.count()
	subject := label.Count(count, "worktree", "worktrees")
	warning := fmt.Sprintf("Komrad deletes %d directories that no agent uses and leaves every branch alone.", count)
	if count == 1 {
		subject = "the worktree of " + worktreeRow{item: deletion.unowned[0]}.name()
		warning = "Komrad deletes the directory, which no agent uses, and leaves its branch alone."
	}

	return worktreeConfirm{
		title:   fmt.Sprintf("Delete %s?", subject),
		warning: warning,
		dirs:    deletion.dirs(),
	}
}

// deleteMixedConfirm asks before task checkouts and other directories are deleted together.
func deleteMixedConfirm(deletion worktreeDeletion, agentCount int) worktreeConfirm {
	lines := []string{
		"Komrad deletes the directories no agent uses and leaves every branch alone.",
	}
	if agentCount > 0 {
		lines = append(lines, "It archives "+label.Count(agentCount, "live agent", "live agents")+" first.")
	}

	return worktreeConfirm{
		title:   fmt.Sprintf("Delete %s?", label.Count(deletion.chosen(), "worktree", "worktrees")),
		warning: strings.Join(lines, " "),
		dirs:    slices.Concat(deletion.dirs(), taskDirs(deletion.tasks)),
		values:  newDeleteFormValues(true),
	}
}
