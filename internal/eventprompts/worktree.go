package eventprompts

import (
	"fmt"
	"strings"

	mailstore "komrad/internal/store/mail"
	"komrad/lib/xsha"
)

// Rebased tells a task's writer how a rebase of its worktree came out.
type Rebased struct {
	BaseRef     string
	BaseSHA     string
	Replay      int
	Landed      int
	Conflicted  bool
	Moved       bool
	Interrupted bool
	ParkedRef   string
}

// Kind is the kind the rebase mail is filed under.
func (Rebased) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is the event the rebase starts, which turns on what became of the branch.
func (m Rebased) Events() []string {
	switch {
	case m.Conflicted:
		return []string{worktreeRebaseConflicted}
	case m.Moved:
		return []string{worktreeLanded}
	default:
		return []string{worktreeRebased}
	}
}

// Body tells the agent what became of its branch rather than what the person asked for.
func (m Rebased) Body() string {
	var body string
	switch {
	case m.Conflicted:
		body = m.describeConflict()
	case m.Moved:
		body = m.describeMove()
	default:
		body = m.describeReplay()
	}

	return m.interruptedNote() + body + m.carryOnNote()
}

func (m Rebased) interruptedNote() string {
	if !m.Interrupted {
		return ""
	}

	return "Your turn was interrupted so that your worktree could be rebased onto its base.\n\n"
}

func (m Rebased) carryOnNote() string {
	if !m.Interrupted || m.Conflicted {
		return ""
	}

	return "\n\nRead your files again, then pick up where you were."
}

// parkNote says where the checkout as it stood beforehand is kept, which is the only way back
// to a commit the replay dropped or a file that was never committed.
func (m Rebased) parkNote() string {
	if m.ParkedRef == "" {
		return ""
	}

	return "\n\nA copy of the worktree from before this ran is kept at " + m.ParkedRef +
		". It holds anything you had not committed, and every commit that is no longer on the branch."
}

// describeConflict tells of a rebase stopped part-way, which is the agent's to settle and nobody else's.
func (m Rebased) describeConflict() string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your worktree was being rebased onto %s (%s). The rebase hit conflicts and stopped part way, so it is still in progress.\n\n",
		m.BaseRef, xsha.Short(m.BaseSHA))
	body.WriteString("All your commits are still there. Your branch has not moved. " +
		"Do not resolve the conflicts and do not abort the rebase on your own. " +
		"Say what state you are in and wait to be told what to do. " +
		"The person who pressed the key decides whether this work is worth carrying forward.")

	body.WriteString("\n\nSay also whether you have a pull request open on this branch. " +
		"Finishing the rebase rewrites the commits it was opened from. " +
		"That invalidates an open pull request. It means nothing for one that has already merged.")
	body.WriteString(m.parkNote())

	return body.String()
}

// describeMove tells of a branch that reached the base without replaying anything, which is what a branch
// whose work has entirely landed gets.
func (m Rebased) describeMove() string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your branch is now on %s (%s).\n\n", m.BaseRef, xsha.Short(m.BaseSHA))
	body.WriteString("Your work had already been merged into the base. " +
		"There was nothing left to replay, so your branch was moved straight onto it.\n\n")
	body.WriteString("Nothing was lost. Your own commits are no longer on the branch and the base's history " +
		"has taken their place. Start your next piece of work from here.")
	body.WriteString("\n\nAny pull request opened from this branch no longer tracks anything it carries.")
	body.WriteString(m.parkNote())

	return body.String()
}

// describeReplay tells of the ordinary rebase, which moved the agent's files under it.
func (m Rebased) describeReplay() string {
	var body strings.Builder

	if m.Replay == 0 {
		fmt.Fprintf(&body, "Your worktree is up to date with %s (%s). You had no commits of your own to replay.\n\n",
			m.BaseRef, xsha.Short(m.BaseSHA))
	} else {
		fmt.Fprintf(&body, "Your worktree was rebased onto %s (%s), replaying %d of your own commits.\n\n",
			m.BaseRef, xsha.Short(m.BaseSHA), m.Replay)
	}
	body.WriteString("Your files may have changed under you. Read them again rather than trusting what you remember. ")
	if m.Landed > 0 {
		fmt.Fprintf(&body, "%d of your commits are no longer on the branch. "+
			"The base already held those changes, so replaying them would have collided with themselves. ", m.Landed)
	}
	body.WriteString("Your branch is still the same branch.")

	body.WriteString("\n\nThe rebase rewrote the commits any pull request on this branch was opened from, " +
		"so do not assume one you opened earlier still tracks your work.")
	body.WriteString(m.parkNote())

	return body.String()
}

// RebaseFailed tells a task's writer that komrad stopped it for a rebase that did not go through.
type RebaseFailed struct {
	Failure string
}

// Kind is the kind the rebase mail is filed under.
func (RebaseFailed) Kind() mailstore.Kind {
	return mailstore.KindWorktreeRebased
}

// Events is the event a failed rebase starts.
func (RebaseFailed) Events() []string {
	return []string{worktreeRebaseFailed}
}

// Body tells the agent its turn was cut for nothing.
func (m RebaseFailed) Body() string {
	return fmt.Sprintf("Your turn was interrupted so that your worktree could be rebased onto its base, "+
		"but the rebase failed before it finished: %s\n\n"+
		"Check git status before carrying on, then pick up where you were.", m.Failure)
}

// Pushed tells a task's writer that the panel overwrote its branch on origin.
type Pushed struct {
	Branch string
	SHA    string
}

// Kind is the kind the push mail is filed under.
func (Pushed) Kind() mailstore.Kind {
	return mailstore.KindWorktreePushed
}

// Events is empty, since a push from the panel starts no event.
func (Pushed) Events() []string {
	return nil
}

// Body tells the agent where origin's copy of its branch now stands.
func (m Pushed) Body() string {
	return fmt.Sprintf("Your branch %s was force-pushed to origin from the panel, replacing the copy origin held. "+
		"Origin's copy now matches your checkout at %s.\n\n"+
		"Carry on from here and push as you normally would. Do not pull or reset to the copy origin held before.",
		m.Branch, xsha.Short(m.SHA))
}

// Aborted tells a task's writer that the panel aborted what its worktree had part-way through.
type Aborted struct {
	Command     string
	Branch      string
	ParkedRef   string
	Ours        bool
	Interrupted bool
}

// Kind is the kind the abort mail is filed under.
func (Aborted) Kind() mailstore.Kind {
	return mailstore.KindWorktreeAborted
}

// Events is empty, since an abort starts no event.
func (Aborted) Events() []string {
	return nil
}

// Body tells the agent what was put back, which turns on whether komrad or the agent started the sequence.
func (m Aborted) Body() string {
	var body strings.Builder

	if m.Interrupted {
		fmt.Fprintf(&body, "Your turn was interrupted so that the %s in your worktree could be aborted.\n\n", m.Command)
	}

	if m.Ours {
		fmt.Fprintf(&body, "The rebase of your worktree that stopped on conflicts was aborted from the panel. "+
			"The person chose not to carry it forward. %s is back where it stood before the rebase began, with all your commits.\n\n"+
			"Do not start the rebase again.", m.standing())
	} else {
		fmt.Fprintf(&body, "The %s you had part-way through your worktree was aborted from the panel. "+
			"%s is back where it stood before it began.\n\n"+
			"Do not start it again without asking.", m.Command, m.standing())
	}

	body.WriteString(" Read your files again rather than trusting what you remember, then carry on from here.")
	body.WriteString("\n\nA copy of the worktree as it stood before the abort, conflicts and any resolution you had made included, is kept at " +
		m.ParkedRef + ".")

	return body.String()
}

func (m Aborted) standing() string {
	if m.Branch == "" {
		return "Your checkout"
	}

	return "Your branch " + m.Branch
}

// AbortFailed tells a task's writer that an abort which cut its turn failed.
type AbortFailed struct {
	Failure string
}

// Kind is the kind the abort mail is filed under.
func (AbortFailed) Kind() mailstore.Kind {
	return mailstore.KindWorktreeAborted
}

// Events is empty, since an abort starts no event.
func (AbortFailed) Events() []string {
	return nil
}

// Body tells the agent its turn was cut for nothing.
func (m AbortFailed) Body() string {
	return fmt.Sprintf("Your turn was interrupted so that what was part-way through your worktree could be aborted, "+
		"but the abort failed: %s\n\n"+
		"Check git status before carrying on, then pick up where you were.", m.Failure)
}
