// Package notices names the things komrad tells an agent, which a workflow can attach a prompt to.
package notices

import "slices"

// MaxPrompt is the most characters one workflow prompt carries.
const MaxPrompt = 500

// The notices komrad sends.
const (
	PullRequestChecksFailing    = "pull_request.checks_failing"
	PullRequestChangesRequested = "pull_request.changes_requested"
	PullRequestApproved         = "pull_request.approved"
	PullRequestConflicting      = "pull_request.conflicting"
	PullRequestBehind           = "pull_request.behind"
	PullRequestMerged           = "pull_request.merged"
	PullRequestClosed           = "pull_request.closed"
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

// Notice is one notice as the panel offers it.
type Notice struct {
	Name        string
	Title       string
	Description string
}

// All is every notice, in the order the panel lists them.
func All() []Notice {
	return []Notice{
		{Name: PullRequestChecksFailing, Title: "Checks failing", Description: "checks on the head are failing"},
		{Name: PullRequestChangesRequested, Title: "Changes requested on the forge", Description: "the forge review decision asks for changes"},
		{Name: PullRequestApproved, Title: "Approved on the forge", Description: "the forge review decision is approved"},
		{Name: PullRequestConflicting, Title: "Conflicting", Description: "the branch conflicts with its base"},
		{Name: PullRequestBehind, Title: "Behind", Description: "the branch is behind its base"},
		{Name: PullRequestMerged, Title: "Merged", Description: "the pull request was merged"},
		{Name: PullRequestClosed, Title: "Closed", Description: "the pull request was closed unmerged"},
		{Name: WorktreeRebased, Title: "Rebased", Description: "the worktree was rebased and its commits replayed"},
		{Name: WorktreeLanded, Title: "Landed", Description: "the work was already in the base, so the branch moved onto it"},
		{Name: WorktreeRebaseConflicted, Title: "Rebase conflicted", Description: "a rebase stopped on conflicts"},
		{Name: WorktreeRebaseFailed, Title: "Rebase failed", Description: "a rebase failed before it finished"},
		{Name: DocumentWrittenByPerson, Title: "Document written by a person", Description: "a person wrote or edited the agent's document"},
		{Name: ReviewApproved, Title: "Review approved", Description: "a reviewer approved the agent's work"},
		{Name: ReviewChangesRequested, Title: "Review asks for changes", Description: "a reviewer asked for changes"},
		{Name: ReviewCommented, Title: "Review commented", Description: "a reviewer released comments with no verdict either way"},
		{Name: ReviewCommentedByPerson, Title: "Person commented", Description: "a person released comments on the agent's work"},
		{Name: ReviewAnswered, Title: "Review answered", Description: "the agent under review answered in a thread, read by its reviewer"},
	}
}

// Names is the name of every notice, in the order All lists them.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, notice := range all {
		names[i] = notice.Name
	}

	return names
}

// Known says whether name is a notice komrad sends.
func Known(name string) bool {
	return slices.Contains(Names(), name)
}

// Named is the notices of names that also hold, in the order they were named and each once.
func Named(names, holding []string) []string {
	var named []string
	for _, name := range names {
		if slices.Contains(holding, name) && !slices.Contains(named, name) {
			named = append(named, name)
		}
	}

	return named
}
