package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"weave/lib/dates"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// pullRequestStanding is where the pull request opened from an agent's branch stands, as the
// listing carries it: the state, and the current one's number, address and title when there is
// one.
type pullRequestStanding struct {
	state  pullrequestv1.PullRequestState
	number int32
	url    string
	title  string
}

// agentPullRequest is one pull request opened from an agent's branch as the record lists it.
type agentPullRequest struct {
	number   int32
	url      string
	title    string
	state    pullrequestv1.PullRequestState
	current  bool
	openedAt time.Time
	mergedAt *time.Time
	closedAt *time.Time
}

// agentPullRequests is what list_agent_pull_requests answered for one agent.
type agentPullRequests struct {
	// pending says a fresh answer is on its way, and answered that one has come at all.
	pending  bool
	answered bool
	// standing is what the listing said when the answer was asked for, so a row whose standing
	// has moved since is asked again.
	standing pullRequestStanding
	readAt   *time.Time
	items    []agentPullRequest
	err      error
}

type pullRequestsState struct {
	// listed is what the service answered for each agent, kept while the agent is listed.
	listed map[string]agentPullRequests
}

// agentPullRequestsListedMsg is one agent's pull requests come back.
type agentPullRequestsListedMsg struct {
	agentID string
	listed  agentPullRequests
}

// listPullRequestsCmd asks the service for the hovered agent's pull requests, answered under the
// agent's own id, and asks nothing while an answer for it is still on its way.
func (m *model) listPullRequestsCmd() tea.Cmd {
	item, ok := m.hoveredPullRequestAgent()
	if !ok {
		return nil
	}
	listed := m.pullRequests.listed[item.agent.ID]
	if listed.pending {
		return nil
	}

	listed.pending = true
	listed.standing = item.pullRequest
	m.applyAgentPullRequests(agentPullRequestsListedMsg{agentID: item.agent.ID, listed: listed})
	clients, agentID, standing := m.api, item.agent.ID, item.pullRequest

	return func() tea.Msg {
		read, err := clients.listAgentPullRequests(agentID)
		if err != nil {
			return agentPullRequestsListedMsg{agentID: agentID, listed: agentPullRequests{
				answered: true,
				standing: standing,
				err:      err,
			}}
		}

		listed := agentPullRequestsFrom(read)
		listed.standing = standing

		return agentPullRequestsListedMsg{agentID: agentID, listed: listed}
	}
}

// listHoveredPullRequestsCmd asks for the hovered agent's pull requests where nothing has been
// asked for it yet, or where its row's standing has moved since the answer.
func (m *model) listHoveredPullRequestsCmd() tea.Cmd {
	item, ok := m.hoveredPullRequestAgent()
	if !ok {
		return nil
	}
	if listed, asked := m.pullRequests.listed[item.agent.ID]; asked && listed.standing == item.pullRequest {
		return nil
	}

	return m.listPullRequestsCmd()
}

// hoveredPullRequestAgent is the agent whose pull requests the record under the agents list is
// about, and none on any other screen or row.
func (m model) hoveredPullRequestAgent() (agentItem, bool) {
	if m.screen != screenAgents {
		return agentItem{}, false
	}

	row, ok := m.hoveredRow()
	if !ok || row.header {
		return agentItem{}, false
	}

	return row.item, true
}

func (m *model) applyAgentPullRequests(msg agentPullRequestsListedMsg) {
	if m.pullRequests.listed == nil {
		m.pullRequests.listed = map[string]agentPullRequests{}
	}

	m.pullRequests.listed[msg.agentID] = msg.listed
}

// forgetUnlistedPullRequests drops what was answered for agents the listing no longer holds.
func (m *model) forgetUnlistedPullRequests(fresh []agentItem) {
	listedIDs := make(map[string]bool, len(fresh))
	for _, item := range fresh {
		listedIDs[item.agent.ID] = true
	}
	for agentID := range m.pullRequests.listed {
		if !listedIDs[agentID] {
			delete(m.pullRequests.listed, agentID)
		}
	}
}

// pullRequestFields are the record's rows for the branch's pull requests: the current one, and
// the rest when there are any, drawn from the listing's standing until the service has answered
// with the whole branch.
func pullRequestFields(standing pullRequestStanding, listed agentPullRequests) []recordField {
	switch standing.state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD:
		return []recordField{{pullRequestFieldKey, "not read yet"}}
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE:
		return []recordField{{pullRequestFieldKey, "none from this branch"}}
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
	}

	if !listed.answered || listed.err != nil || len(listed.items) == 0 {
		return []recordField{{pullRequestFieldKey, fmt.Sprintf("#%d %s", standing.number, pullRequestStateWord(standing.state))}}
	}

	fields := make([]recordField, 0, 2)
	var rest []string
	for _, pullRequest := range listed.items {
		if pullRequest.current {
			fields = append(fields, recordField{pullRequestFieldKey, currentPullRequestField(pullRequest)})

			continue
		}

		rest = append(rest, fmt.Sprintf("#%d %s", pullRequest.number, pullRequestStateWord(pullRequest.state)))
	}
	if len(rest) > 0 {
		fields = append(fields, recordField{"pull requests", strings.Join(rest, ", ")})
	}

	return fields
}

// currentPullRequestField is the current pull request with how long ago it reached where it
// stands: when it finished for a finished one, when it was opened otherwise.
func currentPullRequestField(pullRequest agentPullRequest) string {
	word := pullRequestStateWord(pullRequest.state)
	switch {
	case pullRequest.mergedAt != nil:
		return fmt.Sprintf("#%d %s %s ago", pullRequest.number, word, dates.Age(*pullRequest.mergedAt))
	case pullRequest.closedAt != nil:
		return fmt.Sprintf("#%d %s %s ago", pullRequest.number, word, dates.Age(*pullRequest.closedAt))
	}

	return fmt.Sprintf("#%d %s, opened %s ago", pullRequest.number, word, dates.Age(pullRequest.openedAt))
}

// pullRequestFieldKey labels the pull request an agent's row stands for.
const pullRequestFieldKey = "pull request"

// pullRequestStateWord is the one word the record uses for a state.
func pullRequestStateWord(state pullrequestv1.PullRequestState) string {
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN:
		return "open"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED:
		return "approved"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED:
		return "changes requested"
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

// openPullRequest hands the hovered agent's current pull request to the browser, or says why
// there is none to open.
func (m model) openPullRequest(item agentItem) (tea.Model, tea.Cmd) {
	standing := item.pullRequest
	if standing.url == "" {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: item.agent.Name + ": no pull request to open",
			detail:  noPullRequestSaid(item),
		})

		return m, nil
	}

	return m, m.openURLCmd(standing.url)
}

// noPullRequestSaid is why the key had nothing to open.
func noPullRequestSaid(item agentItem) string {
	if item.pullRequest.state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		branch := ""
		if item.agent.Branch != nil {
			branch = *item.agent.Branch
		}

		return "Weave has read the repository and nothing has been opened from " + orDash(branch) + "."
	}

	return "Weave has not read this agent's repository yet, or its checkout is not on a forge."
}

// urlOpenedMsg is the browser having been asked to open an address.
type urlOpenedMsg struct {
	target string
	err    error
}

// openURLCmd hands an address to the browser off the update, so a slow opener does not hold the
// panel.
func (m *model) openURLCmd(target string) tea.Cmd {
	opener := m.browser

	return func() tea.Msg {
		return urlOpenedMsg{target: target, err: opener.OpenURL(context.Background(), target)}
	}
}

func (m *model) applyURLOpened(msg urlOpenedMsg) {
	m.report("Could not open "+msg.target, msg.err)
}
