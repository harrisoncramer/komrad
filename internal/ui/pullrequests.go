package ui

import (
	"context"
	"fmt"
	"strings"

	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// pullRequestFields are the record's rows for the branch's pull requests: why there is none,
// or the current one and then the rest when there are any.
func pullRequestFields(standing panel.PullRequestStanding, pullRequests []panel.PullRequest) []recordField {
	isNone := standing.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE
	if len(pullRequests) == 0 {
		if isNone {
			return []recordField{{label: pullRequestFieldKey, value: "none from this branch"}}
		}

		return []recordField{{label: pullRequestFieldKey, value: "not read yet"}}
	}

	current := currentPullRequestField(pullRequests[0])
	if isNone {
		current = "none since " + current
	}

	fields := []recordField{{label: pullRequestFieldKey, value: current}}
	rest := xslice.Map(pullRequests[1:], func(pullRequest panel.PullRequest) string {
		return fmt.Sprintf("#%d %s", pullRequest.Number, pullRequestWords(pullRequest))
	})
	if len(rest) > 0 {
		fields = append(fields, recordField{label: "pull requests", value: strings.Join(rest, ", ")})
	}

	return fields
}

// currentPullRequestField is the current pull request with how long ago it reached where it
// stands: when it finished for a finished one, when it was opened otherwise.
func currentPullRequestField(pullRequest panel.PullRequest) string {
	word := pullRequestWords(pullRequest)
	switch {
	case pullRequest.MergedAt != nil:
		return fmt.Sprintf("#%d %s %s ago", pullRequest.Number, word, label.Age(*pullRequest.MergedAt))
	case pullRequest.ClosedAt != nil:
		return fmt.Sprintf("#%d %s %s ago", pullRequest.Number, word, label.Age(*pullRequest.ClosedAt))
	}

	return fmt.Sprintf("#%d %s, opened %s ago", pullRequest.Number, word, label.Age(pullRequest.OpenedAt))
}

// pullRequestFieldKey labels the pull request an agent's row stands for.
const pullRequestFieldKey = "pull request"

// pullRequestWords is the state word with what is still in the way beside it: a run in
// flight, a branch behind or conflicting, or a rule an approval is waiting on, and a ready
// branch the forge is checking again.
func pullRequestWords(pullRequest panel.PullRequest) string {
	state, checks, mergeability := pullRequest.State, pullRequest.Checks, pullRequest.Mergeability
	word := pullRequestStateWord(state)
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT:
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
		return word
	}

	switch {
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED && pullRequest.MergeabilityUnconfirmed:
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
	}

	return ""
}

// openPullRequest hands the agent's current pull request to the browser, or says why there is
// none to open.
func (m model) openPullRequest(item panel.AgentRow) (model, tea.Cmd) {
	standing := item.PullRequest
	if standing.URL == "" {
		m.report("Could not open the pull request of "+item.Agent.Name, pullRequestRefusal(item))

		return m, nil
	}

	return m, m.openURLCmd(standing.URL)
}

// copyPullRequestURL puts the agent's current pull request's address on the clipboard, or says
// why there is none to copy.
func (m model) copyPullRequestURL(item panel.AgentRow) (model, tea.Cmd) {
	standing := item.PullRequest
	if standing.URL == "" {
		m.report("Could not copy the pull request of "+item.Agent.Name, pullRequestRefusal(item))

		return m, nil
	}

	return m, m.copyCmd(standing.URL)
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
