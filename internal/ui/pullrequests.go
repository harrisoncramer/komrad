package ui

import (
	"context"
	"strings"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// pullRequestFields are the record's rows for the branch's pull requests: the current one's title
// and author, then the rest, all of them when the branch has moved past its last one.
func pullRequestFields(standing panel.PullRequestStanding, pullRequests []panel.PullRequest) []recordField {
	if len(pullRequests) == 0 {
		return nil
	}

	var fields []recordField
	rest := pullRequests
	if standing.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		current := pullRequests[0]
		rest = pullRequests[1:]
		if current.Title != "" {
			fields = append(fields, recordField{label: "title", value: current.Title})
		}
		if current.Author != "" {
			fields = append(fields, recordField{label: "author", value: "@" + current.Author})
		}
	}
	if len(rest) == 0 {
		return fields
	}

	words := xslice.Map(rest, func(pullRequest panel.PullRequest) string {
		return pullRequest.Ref() + " " + pullRequestWords(pullRequest)
	})

	return append(fields, recordField{label: rest[0].Noun() + "s", value: strings.Join(words, ", ")})
}

// pullRequestWords is the state word with what is still in the way beside it: a run in
// flight, a branch behind or conflicting, or a rule an approval is waiting on, a ready branch
// the forge is checking again, and failing checks with others still running.
func pullRequestWords(pullRequest panel.PullRequest) string {
	state, checks, mergeability := pullRequest.State, pullRequest.MergeReadiness.Checks, pullRequest.MergeReadiness.Mergeability
	word := pullRequestStateWord(state)
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT:
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING:
		if checks == pullrequestv1.Checks_CHECKS_FAILING_RUNNING {
			return word + ", others still running"
		}

		return word
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
		return word
	}

	switch {
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED && pullRequest.MergeReadiness.MergeabilityUnconfirmed:
		return word + ", forge rechecking"
	case mergeability == pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING:
		return word + ", conflicts with the base"
	case mergeability == pullrequestv1.Mergeability_MERGEABILITY_BEHIND:
		return word + ", behind the base"
	case checks == pullrequestv1.Checks_CHECKS_PENDING:
		return word + ", checks running"
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING && mergeability == pullrequestv1.Mergeability_MERGEABILITY_BLOCKED:
		return word + ", blocked by a rule"
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING && mergeability == pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN:
		return word + ", forge still computing"
	}

	return word
}

// describePullRequest says something of an agent's pull request in its forge's words.
func describePullRequest(standing panel.PullRequestStanding, said string) string {
	if said == "" {
		return ""
	}

	return "its " + standing.Noun() + " " + said
}

// pullRequestStateWord is the one word the record uses for a state.
func pullRequestStateWord(state pullrequestv1.PullRequestState) string {
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN:
		return "open"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING:
		return "checks failing"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING:
		return "conflicts with the base"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING:
		return "approved"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED:
		return "ready to merge"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED:
		return "changes requested"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED:
		return "re-review requested"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING:
		return "checks running"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT:
		return "draft"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED:
		return "merged"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
		return "closed"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE:
		return ""
	}

	return "unknown"
}

// openPullRequest hands the agent's current pull request to the browser, or says why there is
// none to open.
func (m *model) openPullRequest(item panel.AgentRow) tea.Cmd {
	standing := item.PullRequest
	if standing.URL == "" {
		m.report("Could not open the pull request of "+item.Agent.Name, pullRequestRefusal(item))

		return nil
	}

	return m.openURLCmd(standing.URL)
}

// copyPullRequestURL puts the agent's current pull request's address on the clipboard, or says
// why there is none to copy.
func (m *model) copyPullRequestURL(item panel.AgentRow) tea.Cmd {
	standing := item.PullRequest
	if standing.URL == "" {
		m.report("Could not copy the pull request of "+item.Agent.Name, pullRequestRefusal(item))

		return nil
	}

	return m.copyCmd(standing.URL)
}

// urlOpenedMsg is the browser having been asked to open an address.
type urlOpenedMsg struct {
	target string
	err    error
}

// openURLCmd hands an address to the browser off the update.
func (m *model) openURLCmd(target string) tea.Cmd {
	opener := m.browser

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		return urlOpenedMsg{target: target, err: opener.OpenURL(ctx, target)}
	}
}

func (m *model) applyURLOpened(msg urlOpenedMsg) {
	m.report("Could not open "+msg.target, msg.err)
}
