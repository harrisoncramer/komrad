package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/paint/label"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// lostFilesShown is how many uncommitted files a delete confirm names before it counts the rest.
const lostFilesShown = 10

// worktreeConfirm is a delete confirm that says what the worktrees the delete takes still hold.
type worktreeConfirm struct {
	kind    formKind
	title   string
	warning string
	dirs    []string
	// confirmed is the answer the confirm starts on.
	confirmed bool
}

// open reads each worktree afresh and puts the confirm on screen, running write once it is answered yes.
func (c worktreeConfirm) open(m *model, write func(*model) tea.Cmd) tea.Cmd {
	confirmed := c.confirmed

	var cmds []tea.Cmd
	for _, dir := range c.dirs {
		if !m.inspections[dir].isSettled() {
			cmds = append(cmds, m.readWorktreeCmd(dir))
		}
	}

	opened := m.openForm(formState{
		kind:   c.kind,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !confirmed {
				return nil
			}

			return write(m)
		}),
		rebuild: func(m *model) (*huh.Form, []string) {
			form := confirmFormStartingOn(confirmFormStartingOnParams{
				answer:      &confirmed,
				title:       c.title,
				description: c.warning + worktreeLoss(m.inspections, c.dirs),
				affirmative: "Delete",
				keys:        fixedKeys(),
			})

			return form, []string{"confirm"}
		},
	})

	return tea.Batch(append(cmds, opened)...)
}

// rerenderWorktreeConfirm renders a delete confirm again once git answers for a worktree.
func (m *model) rerenderWorktreeConfirm() tea.Cmd {
	if m.form.kind != formArchiveDelete && m.form.kind != formWorktreesDelete {
		return nil
	}

	return m.rerenderOpenForm()
}

// worktreeLoss is what is uncommitted and unpushed in the worktrees, as git last answered for each.
func worktreeLoss(inspections map[string]worktreeInspection, dirs []string) string {
	var files, refusals []string
	var commits, uncounted, read, reading, settled int
	for _, dir := range dirs {
		inspection, asked := inspections[dir]
		switch {
		case !asked || !inspection.answered || inspection.pending:
			reading++
		case inspection.isSettled():
			settled++
		case inspection.err != nil:
			refusals = append(refusals, errui.GetReason(inspection.err))
		case !inspection.item.readable:
			refusals = append(refusals, "git cannot read it")
		default:
			read++
			files = slices.Concat(files, inspection.modified, inspection.untracked)
			if unpushed, ok := inspection.unpushedCommits(); ok {
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
	counts := lossCounts(len(files), commits, uncounted == 0)
	switch {
	case read == 0:
	case worktrees == 1:
		fmt.Fprintf(&b, " The worktree has %s.", counts)
	case read == worktrees:
		fmt.Fprintf(&b, " The worktrees have %s.", counts)
	default:
		fmt.Fprintf(&b, " The %s %s.", label.Count(read, "worktree Komrad read has", "worktrees Komrad read have"), counts)
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
		writeLostFiles(&b, files)
	}

	return b.String()
}

// lossCounts is how many uncommitted files and unpushed commits there are, as far as they were counted.
func lossCounts(files, commits int, counted bool) string {
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

// writeLostFiles lists the uncommitted files one to a line, counting those past lostFilesShown.
func writeLostFiles(b *strings.Builder, files []string) {
	for _, file := range files[:min(len(files), lostFilesShown)] {
		b.WriteString("\n  " + file)
	}
	if hidden := len(files) - lostFilesShown; hidden > 0 {
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
		kind:    formWorktreesDelete,
		title:   fmt.Sprintf("Delete %s?", subject),
		warning: warning,
		dirs:    deletion.dirs(),
	}
}

// deleteMixedConfirm asks before task checkouts and other directories are deleted together.
func deleteMixedConfirm(deletion worktreeDeletion, agentCount int) worktreeConfirm {
	lines := []string{
		"Komrad deletes each task's worktree for good, along with the task's agents, documents and reviews.",
		"It deletes the directories no agent uses and leaves every branch alone.",
	}
	if agentCount > 0 {
		lines = append(lines, "It archives "+label.Count(agentCount, "live agent", "live agents")+" first.")
	}

	return worktreeConfirm{
		kind:    formWorktreesDelete,
		title:   fmt.Sprintf("Permanently delete %s?", label.Count(deletion.chosen(), "worktree", "worktrees")),
		warning: strings.Join(lines, " "),
		dirs:    slices.Concat(deletion.dirs(), taskDirs(deletion.tasks)),
	}
}
