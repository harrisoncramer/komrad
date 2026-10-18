package eventprompts

import (
	"fmt"
	"strings"

	mailstore "komrad/internal/store/mail"
	"komrad/lib/xsha"
)

// Restacked tells a standing task's writer that komrad replayed its commits onto the task it stands on, or onto the base branch once that task merged.
type Restacked struct {
	// Parent is the slug of the task stood on.
	Parent string
	// Onto is the branch the commits now sit on.
	Onto   string
	SHA    string
	Replay int
	// Merged says the parent merged, so the task now stands on the base branch.
	Merged bool
	Pushed bool
}

// Kind is the kind the restack mail is filed under.
func (Restacked) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is the event a restack komrad could not push starts, and none once komrad pushed it.
func (m Restacked) Events() []string {
	if m.Pushed {
		return nil
	}

	return []string{worktreeRebased}
}

// Body tells the agent where its commits now sit and what is left for it to do.
func (m Restacked) Body() string {
	moved := fmt.Sprintf("%s moved to %s.", m.Parent, xsha.Short(m.SHA))
	if m.Merged {
		moved = fmt.Sprintf("%s merged into %s.", m.Parent, m.Onto)
	}

	pushed := ""
	if m.Pushed {
		pushed = " and pushed"
	}

	onto := "it"
	if m.Merged {
		onto = m.Onto
	}

	body := fmt.Sprintf("%s Komrad replayed your %d commits onto %s%s.", moved, m.Replay, onto, pushed)
	if m.Merged {
		body += fmt.Sprintf(" Point your pull request at %s.", m.Onto)
	}

	return body + "\n\nYour files may have changed under you. Read them again rather than trusting what you remember."
}

// RestackConflicted tells a standing task's writer that replaying its commits onto the task it stands on conflicts.
type RestackConflicted struct {
	// Parent is the slug of the task stood on.
	Parent string
	// Onto is the branch the commits would sit on.
	Onto string
	SHA  string
	// Merged says the parent merged, so the commits were bound for the base branch.
	Merged bool
	// Cutoff is the commit the replay starts after.
	Cutoff string
	Paths  []string
}

// Kind is the kind the restack mail is filed under.
func (RestackConflicted) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is the event a conflicted restack starts.
func (RestackConflicted) Events() []string {
	return []string{worktreeRebaseConflicted}
}

// Body names the exact rebase the agent runs to resolve the conflict itself.
func (m RestackConflicted) Body() string {
	moved := fmt.Sprintf("%s moved to %s", m.Parent, xsha.Short(m.SHA))
	if m.Merged {
		moved = fmt.Sprintf("%s merged into %s", m.Parent, m.Onto)
	}

	conflicts := ""
	if len(m.Paths) > 0 {
		conflicts = " in " + strings.Join(m.Paths, ", ")
	}

	return fmt.Sprintf("%s, and replaying your commits onto it conflicts%s. Your branch is as it was. Run\n"+
		"git rebase --onto %s %s\n"+
		"resolve the conflicts, run the tests, and push with --force-with-lease.",
		moved, conflicts, m.SHA, m.Cutoff)
}

// ParentClosed tells a standing task's writer that the pull request of the task it stands on closed without merging.
type ParentClosed struct {
	// Parent is the slug of the task stood on.
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

// Body tells the agent its task still stands on the parent and that the person decides what follows.
func (m ParentClosed) Body() string {
	return fmt.Sprintf("The pull request of %s, the task yours stands on, closed without merging: %s\n\n"+
		"Your task still stands on %s, and komrad will not move your branch. "+
		"The person can move your task onto the base branch from its menu.", m.Parent, m.URL, m.Parent)
}

// RestackLost tells a standing task's writer that its branch no longer holds the commit of the task it stands on that its own commits began after.
type RestackLost struct {
	// Parent is the slug of the task stood on.
	Parent string
	// Tip is the commit the task's own commits were recorded as sitting on.
	Tip string
}

// Kind is the kind the mail is filed under.
func (RestackLost) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is empty, since a lost stand starts no event.
func (RestackLost) Events() []string {
	return nil
}

// Body tells the agent komrad stopped moving its branch and how the person picks it up again.
func (m RestackLost) Body() string {
	return fmt.Sprintf("Your branch no longer holds %s, the commit of %s your own commits sat on, "+
		"so komrad stopped moving your branch with %s.\n\n"+
		"The person can name the commit your own work starts from, or move your task onto the base branch, from its menu.",
		xsha.Short(m.Tip), m.Parent, m.Parent)
}
