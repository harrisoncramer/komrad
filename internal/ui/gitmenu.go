package ui

import (
	"errors"
	"slices"

	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// gitChoice is one entry of the git menu.
type gitChoice int

const (
	gitChoiceRebase gitChoice = iota
	gitChoiceRebaseOntoBase
	gitChoicePush
	gitChoiceOpenPullRequest
	gitChoiceCopyPullRequestURL
	gitChoiceToggleDraft
	gitChoiceRequestRereview
)

var (
	errArchivedWorktree     = errors.New("komrad leaves an archived task's worktree alone")
	errNoWorktree           = errors.New("this task has no worktree of its own for komrad to act on")
	errPushInProgress       = errors.New("the worktree is part-way through a rebase, merge or pick, so finish or abort that first")
	errAbortNothing         = errors.New("nothing is part-way through in this worktree, so there is nothing to abort")
	errAgentGone            = errors.New("the agent the menu was opened on has left the list")
	errTaskGone             = errors.New("the task the menu was opened on has left the list")
	errNoPullRequest        = errors.New("nobody has opened a pull request from this branch")
	errMovedPastPullRequest = errors.New("the branch moved on after its last pull request finished, and nobody has opened a new one")
	errPullRequestsUnread   = errors.New("komrad has not read this task's repository yet, or its checkout is not on a forge")
	errNoParent             = errors.New("this task does not depend on another task, so it is already on the base branch")
)

// gitMenu is the git actions on a task's checkout and pull request.
func gitMenu(m *model, task panel.Task) submenu {
	taskID := task.ID

	worktreeEntries := []submenuEntry{
		{
			label: rebaseLabel(m.listing, task),
			keys:  m.keys.Git.Rebase,
			isOff: worktreeRefusal(m.listing, task) != nil,
			write: gitWrite(taskID, gitChoiceRebase),
		},
		{
			label: "re-target onto base",
			keys:  m.keys.Git.RebaseOntoBase,
			isOff: rebaseOntoBaseRefusal(m.listing, task) != nil,
			write: gitWrite(taskID, gitChoiceRebaseOntoBase),
		},
		{
			label: "pick fork point",
			keys:  m.keys.Git.PickForkPoint,
			isOff: pickForkPointRefusal(m.listing, task) != nil,
			open:  func(m *model) tea.Cmd { return m.openGitPickForkPoint(taskID) },
		},
		{
			label: abortLabel(task),
			keys:  m.keys.Git.Abort,
			isOff: abortRefusal(m.listing, task) != nil,
			open:  func(m *model) tea.Cmd { return m.openGitAbort(taskID) },
		},
	}

	return submenu{
		kind:    formGit,
		title:   "git",
		entries: slices.Concat(worktreeEntries, pullRequestEntries(m, task)),
	}
}

// pullRequestGitMenu is the git menu holding only a task's pull request entries.
func pullRequestGitMenu(m *model, task panel.Task) submenu {
	return submenu{
		kind:    formGit,
		title:   "git",
		entries: pullRequestEntries(m, task),
	}
}

// pullRequestEntries are the git menu's entries that push a task's branch or act on its pull request.
func pullRequestEntries(m *model, task panel.Task) []submenuEntry {
	return []submenuEntry{
		{
			label: "push",
			keys:  m.keys.Git.Push,
			isOff: pushRefusal(m.listing, task) != nil,
			write: gitWrite(task.ID, gitChoicePush),
		},
		{
			label: "open " + task.PullRequest.Abbrev(),
			keys:  m.keys.Git.OpenPullRequest,
			isOff: task.PullRequest.URL == "",
			write: gitWrite(task.ID, gitChoiceOpenPullRequest),
		},
		{
			label: "copy " + task.PullRequest.Abbrev() + " url",
			keys:  m.keys.Git.CopyPullRequestURL,
			isOff: task.PullRequest.URL == "",
			write: gitWrite(task.ID, gitChoiceCopyPullRequestURL),
		},
		{
			label: draftLabel(task),
			keys:  m.keys.Git.ToggleDraft,
			isOff: openPullRequestRefusal(task) != nil,
			write: gitWrite(task.ID, gitChoiceToggleDraft),
		},
		{
			label: "re-request review",
			keys:  m.keys.Git.RequestRereview,
			isOff: rereviewRefusal(task) != nil,
			write: gitWrite(task.ID, gitChoiceRequestRereview),
		},
		nestedEntry("merge "+task.PullRequest.Abbrev(), m.keys.Git.Merge, mergeMenu(m, task)),
	}
}

// gitWrite is the write a git menu entry makes on a task.
func gitWrite(taskID string, choice gitChoice) func(*model) tea.Cmd {
	return func(m *model) tea.Cmd { return m.runGitChoice(taskID, choice) }
}

// worktreeRefusal returns the reason a task's worktree refuses an action, or nil when it allows one.
func worktreeRefusal(l panel.Listing, task panel.Task) error {
	switch {
	case task.ArchivedAt != nil:
		return errArchivedWorktree
	case !l.HasOwnWorktree(task):
		return errNoWorktree
	}

	return nil
}

// rebaseLabel names the rebase entry for the branch it moves onto.
func rebaseLabel(l panel.Listing, task panel.Task) string {
	if task.Parent == nil {
		return "rebase with base"
	}

	return "rebase onto " + l.Tasks[task.Parent.GetTaskId()].Slug
}

// rebaseOntoBaseRefusal returns the reason a task refuses a re-target onto the base branch, or nil when it allows one.
func rebaseOntoBaseRefusal(l panel.Listing, task panel.Task) error {
	if err := worktreeRefusal(l, task); err != nil {
		return err
	}
	if task.Parent == nil {
		return errNoParent
	}

	return nil
}

// pushRefusal returns the reason a task's branch refuses a push, or nil when it allows one.
func pushRefusal(l panel.Listing, task panel.Task) error {
	if err := worktreeRefusal(l, task); err != nil {
		return err
	}
	if task.Sequence.IsInProgress() {
		return errPushInProgress
	}

	return nil
}

// mergeRebaseRefusal returns the reason a task refuses a merge or a rebase, or nil when it allows
// both.
func mergeRebaseRefusal(l panel.Listing, task panel.Task) error {
	if err := pushRefusal(l, task); err != nil {
		return err
	}

	return openPullRequestRefusal(task)
}

// abortRefusal returns the reason a task refuses an abort, or nil when its worktree has an
// operation to abort.
func abortRefusal(l panel.Listing, task panel.Task) error {
	if err := worktreeRefusal(l, task); err != nil {
		return err
	}
	if !task.Sequence.IsInProgress() {
		return errAbortNothing
	}

	return nil
}

// abortLabel names the abort entry for what it would undo.
func abortLabel(task panel.Task) string {
	if !task.Sequence.IsInProgress() {
		return "abort"
	}

	return "abort " + string(task.Sequence)
}

// pullRequestRefusal is why a task has no current pull request.
func pullRequestRefusal(task panel.Task) error {
	switch {
	case task.PullRequest.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE:
		return errPullRequestsUnread
	case len(task.PullRequests) > 0:
		return errMovedPastPullRequest
	default:
		return errNoPullRequest
	}
}

// openPullRequestRefusal is why a task has no open pull request to act on, nil when it has one.
func openPullRequestRefusal(task panel.Task) error {
	if task.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED {
		return errors.New(task.PullRequest.Describe("has already merged"))
	}
	if task.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED {
		return errors.New(task.PullRequest.Describe("was closed without merging"))
	}

	if task.PullRequest.URL == "" {
		return pullRequestRefusal(task)
	}

	return nil
}

// runGitChoice does what the entry asked for, or says why this task is not one to ask it of.
func (m *model) runGitChoice(taskID string, choice gitChoice) tea.Cmd {
	task, ok := m.findGitTask(taskID)
	if !ok {
		m.report("Could not act on the task", errTaskGone)

		return nil
	}

	switch choice {
	case gitChoiceRebase:
		if err := worktreeRefusal(m.listing, task); err != nil {
			m.report("Could not rebase the worktree of "+task.Slug, err)

			return nil
		}

		return m.planGitCmd(rebaseOp(task.ID, task.Slug, false))
	case gitChoiceRebaseOntoBase:
		if err := rebaseOntoBaseRefusal(m.listing, task); err != nil {
			m.report("Could not re-target "+task.Slug+" onto the base branch", err)

			return nil
		}

		return m.planGitCmd(rebaseOp(task.ID, task.Slug, true))
	case gitChoicePush:
		if err := pushRefusal(m.listing, task); err != nil {
			m.report("Could not push the branch of "+task.Slug, err)

			return nil
		}

		return m.planGitCmd(pushOp(task))
	case gitChoiceOpenPullRequest:
		return m.openPullRequest(task)
	case gitChoiceCopyPullRequestURL:
		return m.copyPullRequestURL(task)
	case gitChoiceToggleDraft:
		return m.toggleDraft(task)
	case gitChoiceRequestRereview:
		return m.requestRereview(task)
	}

	return nil
}

// findGitTask returns the task the menu opened on, as the list holds it now.
func (m *model) findGitTask(taskID string) (panel.Task, bool) {
	// The list may have moved under the open menu.
	task, ok := m.listing.Tasks[taskID]

	return task, ok
}

// openGitAbort leaves the menu for the abort's confirmation, or says why there is nothing to abort.
func (m *model) openGitAbort(taskID string) tea.Cmd {
	task, ok := m.findGitTask(taskID)
	if !ok {
		m.report("Could not act on the task", errTaskGone)

		return m.closeForm()
	}

	if err := abortRefusal(m.listing, task); err != nil {
		m.report("Could not abort what the worktree of "+task.Slug+" has in progress", err)

		return m.closeForm()
	}

	m.leaveForm()

	return m.openGitConfirm(abortOp(task.ID, task.Slug, task.Sequence), abortForm(task.Slug, task.Sequence))
}

// openGitPickForkPoint leaves the menu for the form that picks an orphaned child's fork point, or says why the task is not orphaned.
func (m *model) openGitPickForkPoint(taskID string) tea.Cmd {
	task, ok := m.findGitTask(taskID)
	if !ok {
		m.report("Could not act on the task", errTaskGone)

		return m.closeForm()
	}

	if err := pickForkPointRefusal(m.listing, task); err != nil {
		m.report("Could not pick the fork point of "+task.Slug, err)

		return m.closeForm()
	}

	m.leaveForm()

	return openPickForkPoint(m, task)
}
