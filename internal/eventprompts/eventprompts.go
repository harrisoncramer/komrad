// Package eventprompts names the events komrad mails an agent about, which a workflow can attach a prompt to.
package eventprompts

import "slices"

// MaxPrompt is the most characters one workflow prompt carries.
const MaxPrompt = 500

// The events komrad sends.
const (
	PullRequestChecksFailing    = "pull_request.checks_failing"
	PullRequestChangesRequested = "pull_request.changes_requested"
	PullRequestApproved         = "pull_request.approved"
	PullRequestConflicting      = "pull_request.conflicting"
	PullRequestBehind           = "pull_request.behind"
	PullRequestMerged           = "pull_request.merged"
	PullRequestClosed           = "pull_request.closed"
	PullRequestAutoMergeOn      = "pull_request.auto_merge_on"
	WorktreeRebased             = "worktree.rebased"
	WorktreeLanded              = "worktree.landed"
	WorktreeRebaseConflicted    = "worktree.rebase_conflicted"
	WorktreeRebaseFailed        = "worktree.rebase_failed"
	DocumentWrittenByPerson     = "document.written_by_person"
	ReviewApproved              = "review.approved"
	ReviewChangesRequested      = "review.changes_requested"
	ReviewCommented             = "review.commented"
	ReviewCommentedByPerson     = "review.commented_by_person"
	ReviewAnswered              = "review.answered"
)

// Event is one event as the panel offers it.
type Event struct {
	Name          string
	Title         string
	Description   string
	DefaultPrompt string
}

// All is every event, in the order the panel lists them.
func All() []Event {
	return []Event{
		{
			Name: PullRequestChecksFailing, Title: "Checks failing", Description: "checks on the head are failing",
			DefaultPrompt: "Read the failing job's logs, reproduce the failure locally, fix it, run the tests, and push.",
		},
		{
			Name: PullRequestChangesRequested, Title: "Changes requested on the forge", Description: "the forge review decision asks for changes",
			DefaultPrompt: `These are human reviews. Only directly address the "nits" such as variable names, and tiny refactors. Respond "done" in each thread in komrad, push, and say "done" in the remote thread.`,
		},
		{
			Name: PullRequestApproved, Title: "Approved on the forge", Description: "the forge review decision is approved",
			DefaultPrompt: "Leave the pull request as it is. Do not merge it unless your instructions say to.",
		},
		{
			Name: PullRequestConflicting, Title: "Conflicting", Description: "the branch conflicts with its base",
			DefaultPrompt: "Rebase onto the base branch, resolve the conflicts keeping the intent of both sides, run the tests, and push with --force-with-lease. If it's unclear which code to choose from the rebase, stop and ask for help.",
		},
		{
			Name: PullRequestBehind, Title: "Behind", Description: "the branch is behind its base",
			DefaultPrompt: "No action is needed unless the base changed something your work depends on.",
		},
		{
			Name: PullRequestMerged, Title: "Merged", Description: "the pull request was merged",
			DefaultPrompt: "Your work has landed. Stop working on this branch.",
		},
		{
			Name: PullRequestClosed, Title: "Closed", Description: "the pull request was closed unmerged",
			DefaultPrompt: "The pull request was closed without merging. Stop, and do not reopen it unless asked.",
		},
		{
			Name: PullRequestAutoMergeOn, Title: "Auto-merge on", Description: "the forge will merge it once its checks and reviews pass",
			DefaultPrompt: "Anything you push will merge once checks and reviews pass, so push only finished work.",
		},
		{
			Name: WorktreeRebased, Title: "Rebased", Description: "the worktree was rebased and its commits replayed",
			DefaultPrompt: "Run the tests to confirm the replayed commits still build, then push with --force-with-lease.",
		},
		{
			Name: WorktreeLanded, Title: "Landed", Description: "the work was already in the base, so the branch moved onto it",
			DefaultPrompt: "Your work is already in the base. Stop working on this branch.",
		},
		{
			Name: WorktreeRebaseConflicted, Title: "Rebase conflicted", Description: "a rebase stopped on conflicts",
			DefaultPrompt: "Resolve each conflict keeping the intent of both sides, continue the rebase, run the tests, and push with --force-with-lease. If it's unclear which side of the logic is preferred, stop and ask for help.",
		},
		{
			Name: WorktreeRebaseFailed, Title: "Rebase failed", Description: "a rebase failed before it finished",
			DefaultPrompt: "Fix whatever stopped the rebase before carrying on. Do not retry it blindly. If anything is unclear, stop and ask for help.",
		},
		{
			Name: DocumentWrittenByPerson, Title: "Document written by a person", Description: "a person wrote or edited the agent's document",
			DefaultPrompt: "Treat the person's edits as decisions. Build on their version rather than writing over it.",
		},
		{
			Name: ReviewApproved, Title: "Review approved", Description: "a reviewer approved the agent's work",
			DefaultPrompt: "The reviewer is finished. Carry on with your next step.",
		},
		{
			Name: ReviewChangesRequested, Title: "Review asks for changes", Description: "a reviewer asked for changes",
			DefaultPrompt: "Make one change per thread and run the tests before replying addressed.",
		},
		{
			Name: ReviewCommented, Title: "Review commented", Description: "a reviewer released comments with no verdict either way",
			DefaultPrompt: "Answer each comment. Push back where you disagree rather than changing code just to fit.",
		},
		{
			Name: ReviewCommentedByPerson, Title: "Person commented", Description: "a person released comments on the agent's work",
			DefaultPrompt: "A person's comments outrank a reviewer's. Do what they ask, and ask in the thread if anything is unclear.",
		},
		{
			Name: ReviewAnswered, Title: "Review answered", Description: "the agent under review answered in a thread, read by its reviewer",
			DefaultPrompt: "Read the new round against your comment. Resolve the thread if it is answered, and say what is still missing if it is not.",
		},
	}
}

// Names is the name of every event, in the order All lists them.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, event := range all {
		names[i] = event.Name
	}

	return names
}

// IsKnown says whether name is an event komrad sends.
func IsKnown(name string) bool {
	return slices.Contains(Names(), name)
}
