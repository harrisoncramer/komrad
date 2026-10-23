package gitmenu

import (
	"errors"
	"slices"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// gitChoice is one entry of the git or forge menu.
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
	errTaskGone             = errors.New("the task the menu was opened on has left the list")
	errNoPullRequest        = errors.New("nobody has opened a pull request from this branch")
	errMovedPastPullRequest = errors.New("the branch moved on after its last pull request finished, and nobody has opened a new one")
	errPullRequestsUnread   = errors.New("komrad has not read this task's repository yet, or its checkout is not on a forge")
	errNoParent             = errors.New("this task does not depend on another task, so it is already on the base branch")
)

// Deps is what the git menu is given at launch.
type Deps struct {
	Keys    config.Keybindings
	Clients api.Clients
	Browser browser.Opener
}

// New is the git menu built from what it is given at launch.
func New(deps Deps) Menus {
	return Menus{deps: deps}
}

// Menus builds the git menu and the forge menu it nests.
type Menus struct {
	deps Deps
}

// Receive answers a git result that has come back, and reports whether msg was one.
func (g Menus) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case plannedMsg:
		return g.applyPlanned(msg), true
	case doneMsg:
		return applyDone(in, msg), true
	case mergePlannedMsg:
		return g.applyMergePlanned(msg), true
	case urlOpenedMsg:
		return applyURLOpened(msg), true
	}

	return nil, false
}

// Git is the git actions on a task's checkout and pull request.
func (g Menus) Git(l panel.Listing, task panel.Task) menu.Menu {
	taskID := task.ID

	worktreeEntries := []menu.Entry{
		{
			Label:     rebaseLabel(l, task),
			Keys:      g.deps.Keys.Git.Rebase,
			IsDimmed:  worktreeRefusal(l, task) != nil,
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(taskID, gitChoiceRebase),
		},
		{
			Label:     "re-target onto base",
			Keys:      g.deps.Keys.Git.RebaseOntoBase,
			IsDimmed:  rebaseOntoBaseRefusal(l, task) != nil,
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(taskID, gitChoiceRebaseOntoBase),
		},
		{
			Label:    "pick fork point",
			Keys:     g.deps.Keys.Git.PickForkPoint,
			IsDimmed: pickForkPointRefusal(l, task) != nil,
			Pick:     func(in screen.Inputs) screen.Request { return g.openPickForkPoint(in, taskID) },
		},
		{
			Label:    abortLabel(task),
			Keys:     g.deps.Keys.Git.Abort,
			IsDimmed: abortRefusal(l, task) != nil,
			Pick:     func(in screen.Inputs) screen.Request { return g.openAbort(in, taskID) },
		},
	}

	return menu.Menu{
		Title:   "git",
		Entries: slices.Concat(worktreeEntries, g.branchEntries(l, task)),
	}
}

// PullRequestGit is the git menu holding only a task's push and its forge menu.
func (g Menus) PullRequestGit(l panel.Listing, task panel.Task) menu.Menu {
	return menu.Menu{
		Title:   "git",
		Entries: g.branchEntries(l, task),
	}
}

// branchEntries are the git menu's entries that push a task's branch or open the forge menu on its
// pull request.
func (g Menus) branchEntries(l panel.Listing, task panel.Task) []menu.Entry {
	return []menu.Entry{
		{
			Label:     "push",
			Keys:      g.deps.Keys.Git.Push,
			IsDimmed:  pushRefusal(l, task) != nil,
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(task.ID, gitChoicePush),
		},
		menu.Nested("forge", g.deps.Keys.Git.Forge, g.forgeMenu(l, task)),
	}
}

// forgeMenu is the actions on a task's pull request, opened from the git menu, with the ways to
// merge it under a rule.
func (g Menus) forgeMenu(l panel.Listing, task panel.Task) menu.Menu {
	return menu.Join(
		menu.Menu{Title: "forge", Entries: g.pullRequestEntries(task)},
		menu.Menu{Entries: g.mergeEntries(l, task)},
	)
}

// pullRequestEntries are the forge menu's entries that open, copy, draft or re-request review on a
// task's pull request.
func (g Menus) pullRequestEntries(task panel.Task) []menu.Entry {
	return []menu.Entry{
		{
			Label:     "open " + task.PullRequest.Abbrev(),
			Keys:      g.deps.Keys.Forge.OpenPullRequest,
			IsDimmed:  task.PullRequest.URL == "",
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(task.ID, gitChoiceOpenPullRequest),
		},
		{
			Label:     "copy " + task.PullRequest.Abbrev() + " url",
			Keys:      g.deps.Keys.Forge.CopyPullRequestURL,
			IsDimmed:  task.PullRequest.URL == "",
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(task.ID, gitChoiceCopyPullRequestURL),
		},
		{
			Label:     draftLabel(task),
			Keys:      g.deps.Keys.Forge.ToggleDraft,
			IsDimmed:  openPullRequestRefusal(task) != nil,
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(task.ID, gitChoiceToggleDraft),
		},
		{
			Label:     "re-request review",
			Keys:      g.deps.Keys.Forge.RequestRereview,
			IsDimmed:  rereviewRefusal(task) != nil,
			AfterPick: menu.KeepOpenWhileWriting,
			Pick:      g.gitWrite(task.ID, gitChoiceRequestRereview),
		},
	}
}

// gitWrite is the write a git menu entry makes on a task.
func (g Menus) gitWrite(taskID string, choice gitChoice) func(screen.Inputs) screen.Request {
	return func(in screen.Inputs) screen.Request { return g.runGitChoice(in, taskID, choice) }
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
	if panel.IsInProgress(task.Sequence) {
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
	if err := openPullRequestRefusal(task); err != nil {
		return err
	}

	return forgeMergeRefusal(task)
}

// forgeMergeRefusal is why the forge last said it would not merge the task's pull request, nil
// when it said nothing against it.
func forgeMergeRefusal(task panel.Task) error {
	if isDraft(task) {
		return errors.New(task.PullRequest.Describe("is a draft, so mark it ready first"))
	}

	switch task.PullRequest.MergeReadiness.Mergeability {
	case pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING:
		return errors.New(task.PullRequest.Describe("conflicts with the base branch, so rebase it first"))
	case pullrequestv1.Mergeability_MERGEABILITY_BEHIND:
		return errors.New(task.PullRequest.Describe("is behind the base branch, so rebase it first"))
	case pullrequestv1.Mergeability_MERGEABILITY_BLOCKED:
		return errors.New(task.PullRequest.Describe("is blocked by a rule on the forge, so merge it when ready instead"))
	case pullrequestv1.Mergeability_MERGEABILITY_UNSPECIFIED,
		pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
		pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
		pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE:
	}

	return nil
}

// abortRefusal returns the reason a task refuses an abort, or nil when its worktree has an
// operation to abort.
func abortRefusal(l panel.Listing, task panel.Task) error {
	if err := worktreeRefusal(l, task); err != nil {
		return err
	}
	if !panel.IsInProgress(task.Sequence) {
		return errAbortNothing
	}

	return nil
}

// abortLabel names the abort entry for what it would undo.
func abortLabel(task panel.Task) string {
	if !panel.IsInProgress(task.Sequence) {
		return "abort"
	}

	return "abort " + panel.DescribeSequence(task.Sequence)
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
func (g Menus) runGitChoice(in screen.Inputs, taskID string, choice gitChoice) screen.Request {
	task, ok := findTask(in.Listing, taskID)
	if !ok {
		return screen.Report{Summary: "Could not act on the task", Err: errTaskGone}
	}

	switch choice {
	case gitChoiceRebase:
		if err := worktreeRefusal(in.Listing, task); err != nil {
			return screen.Report{Summary: "Could not rebase the worktree of " + task.Slug, Err: err}
		}

		return g.planOperation(rebaseOp(task.ID, task.Slug, false))
	case gitChoiceRebaseOntoBase:
		if err := rebaseOntoBaseRefusal(in.Listing, task); err != nil {
			return screen.Report{Summary: "Could not re-target " + task.Slug + " onto the base branch", Err: err}
		}

		return g.planOperation(rebaseOp(task.ID, task.Slug, true))
	case gitChoicePush:
		if err := pushRefusal(in.Listing, task); err != nil {
			return screen.Report{Summary: "Could not push the branch of " + task.Slug, Err: err}
		}

		return g.planOperation(pushOp(task))
	case gitChoiceOpenPullRequest:
		return g.openPullRequest(task)
	case gitChoiceCopyPullRequestURL:
		return copyPullRequestURL(task)
	case gitChoiceToggleDraft:
		return g.toggleDraft(task)
	case gitChoiceRequestRereview:
		return g.requestRereview(task)
	}

	return nil
}

// findTask returns the task the menu opened on, as the list holds it now.
func findTask(l panel.Listing, taskID string) (panel.Task, bool) {
	// The list may have moved under the open menu.
	task, ok := l.Tasks[taskID]

	return task, ok
}

// openAbort leaves the menu for the abort's confirmation, or says why there is nothing to abort.
func (g Menus) openAbort(in screen.Inputs, taskID string) screen.Request {
	task, ok := findTask(in.Listing, taskID)
	if !ok {
		return screen.Batch{screen.Report{Summary: "Could not act on the task", Err: errTaskGone}, form.Close{}}
	}

	if err := abortRefusal(in.Listing, task); err != nil {
		return screen.Batch{screen.Report{Summary: "Could not abort what the worktree of " + task.Slug + " has in progress", Err: err}, form.Close{}}
	}

	return g.openConfirm(abortOp(task.ID, task.Slug, task.Sequence), abortForm(task.Slug, task.Sequence))
}
