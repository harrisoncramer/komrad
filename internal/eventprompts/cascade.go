package eventprompts

import (
	"fmt"
	"strings"

	mailstore "komrad/internal/store/mail"
	"komrad/lib/xsha"
)

// filesMoved closes the mail to a writer whose files the cascade moved.
const filesMoved = "\n\nYour files may have changed under you. Read them again rather than trusting what you remember."

// CascadeRebased tells a child's writer that the cascade rebased its own commits onto its parent's
// newest commit, or onto the base branch once the parent merged.
type CascadeRebased struct {
	// Parent is the slug of the parent.
	Parent string
	// Onto is the branch the commits now sit on.
	Onto string
	SHA  string
	// Commits is how many of the child's own commits moved.
	Commits int
	// Merged says the parent merged, so the child is now on the base branch.
	Merged bool
	Pushed bool
	// Stacked says the child's pull request sits in a GitHub stack, which changes its base branch itself.
	Stacked bool
}

// Kind is the kind the cascade's mail is filed under.
func (CascadeRebased) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is the event a cascade rebase komrad could not push starts, and none once komrad pushed it.
func (m CascadeRebased) Events() []string {
	if m.Pushed {
		return nil
	}

	return []string{worktreeRebased}
}

// Body tells the agent where its commits now sit and what is left for it to do.
func (m CascadeRebased) Body() string {
	pushed := ""
	if m.Pushed {
		pushed = " and pushed"
	}

	body := fmt.Sprintf("%s moved to %s. Komrad rebased your %d commits onto it%s.", m.Parent, xsha.Short(m.SHA), m.Commits, pushed)
	if m.Merged {
		body = fmt.Sprintf("%s merged into %s. Komrad rebased your %d commits onto %s%s.", m.Parent, m.Onto, m.Commits, m.Onto, pushed)
		if !m.Stacked {
			body += fmt.Sprintf(" Point your pull request at %s.", m.Onto)
		}
	}

	return body + filesMoved
}

// StackRebased tells a child's writer that GitHub rebased its branch in its GitHub stack, and komrad moved its
// worktree to match.
type StackRebased struct {
	// Parent is the slug of the parent.
	Parent string
	// Onto is the branch the commits now sit on.
	Onto string
	SHA  string
	// Commits is how many of the child's own commits sit on Onto.
	Commits int
	// Merged says the parent merged, so the child is now on the base branch.
	Merged bool
}

// Kind is the kind the cascade's mail is filed under.
func (StackRebased) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is empty, since GitHub already pushed the branch.
func (StackRebased) Events() []string {
	return nil
}

// Body tells the agent where GitHub put its commits.
func (m StackRebased) Body() string {
	body := fmt.Sprintf("%s moved to %s. GitHub rebased your %d commits onto it. Komrad moved your worktree to match.", m.Parent, xsha.Short(m.SHA), m.Commits)
	if m.Merged {
		body = fmt.Sprintf("%s merged into %s. GitHub rebased your %d commits onto %s and changed your pull request's base branch to %s. Komrad moved your worktree to match.", m.Parent, m.Onto, m.Commits, m.Onto, m.Onto)
	}

	return body + filesMoved
}

// CascadeConflicted tells a child's writer that rebasing its own commits onto its parent conflicts.
type CascadeConflicted struct {
	// Parent is the slug of the parent.
	Parent string
	// Onto is the branch the commits would sit on.
	Onto string
	SHA  string
	// Merged says the parent merged, so the rebase was onto the base branch.
	Merged bool
	// Cutoff is the commit the rebase starts after.
	Cutoff string
	Paths  []string
}

// Kind is the kind the cascade's mail is filed under.
func (CascadeConflicted) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is the event a conflicted cascade rebase starts.
func (CascadeConflicted) Events() []string {
	return []string{worktreeRebaseConflicted}
}

// Body names the exact rebase the agent runs to resolve the conflict itself.
func (m CascadeConflicted) Body() string {
	moved := fmt.Sprintf("%s moved to %s", m.Parent, xsha.Short(m.SHA))
	if m.Merged {
		moved = fmt.Sprintf("%s merged into %s", m.Parent, m.Onto)
	}

	conflicts := ""
	if len(m.Paths) > 0 {
		conflicts = " in " + strings.Join(m.Paths, ", ")
	}

	return fmt.Sprintf("%s, and rebasing your commits onto it conflicts%s. Your branch is as it was. Run\n"+
		"git rebase --onto %s %s\n"+
		"resolve the conflicts, run the tests, and push with --force-with-lease.",
		moved, conflicts, m.SHA, m.Cutoff)
}

// ParentClosed tells a child's writer that its parent pull request closed without merging.
type ParentClosed struct {
	// Parent is the slug of the parent.
	Parent string
	URL    string
}

// Kind is the kind the mail is filed under.
func (ParentClosed) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is empty, since a closed parent starts no event.
func (ParentClosed) Events() []string {
	return nil
}

// Body tells the agent its task still depends on the parent and that the person decides what follows.
func (m ParentClosed) Body() string {
	return fmt.Sprintf("The pull request of %s, the task yours depends on, closed without merging: %s\n\n"+
		"Your task still depends on %s, and komrad will not rebase your branch. "+
		"The person can re-target your task onto the base branch from its menu.", m.Parent, m.URL, m.Parent)
}

// CascadeOrphaned tells a child's writer that its branch no longer holds its fork point.
type CascadeOrphaned struct {
	// Parent is the slug of the parent.
	Parent string
	// ForkPoint is the fork point the branch no longer holds.
	ForkPoint string
}

// Kind is the kind the mail is filed under.
func (CascadeOrphaned) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is empty, since an orphaned child starts no event.
func (CascadeOrphaned) Events() []string {
	return nil
}

// Body tells the agent the cascade stopped rebasing its branch and how the person starts it again.
func (m CascadeOrphaned) Body() string {
	return fmt.Sprintf("Your branch no longer holds %s, the commit of %s your own commits sat on, "+
		"so komrad stopped rebasing your branch onto %s.\n\n"+
		"The person can pick the commit your own work starts from, or re-target your task onto the base branch, from its menu.",
		xsha.Short(m.ForkPoint), m.Parent, m.Parent)
}
