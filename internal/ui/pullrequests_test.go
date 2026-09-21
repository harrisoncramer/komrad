package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/lib/xptr"
	"weave/lib/xslice"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullRequestFields covers the record's rows for the branch's pull requests: what the two
// blank standings say, the current one drawn from the listing until the service answers, and
// the whole branch once it has, the rest folded into one row.
func TestPullRequestFields(t *testing.T) {
	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	yesterday := now.Add(-26 * time.Hour)

	tests := []struct {
		name     string
		standing pullRequestStanding
		listed   agentPullRequests
		want     []recordField
	}{
		{
			name:     "a repository not read yet",
			standing: pullRequestStanding{state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD},
			want:     []recordField{{"pull request", "not read yet"}},
		},
		{
			name:     "a branch with none",
			standing: pullRequestStanding{state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE},
			want:     []recordField{{"pull request", "none from this branch"}},
		},
		{
			name: "the current one from the listing while the service has not answered",
			standing: pullRequestStanding{
				state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
				number: 741,
			},
			want: []recordField{{"pull request", "#741 changes requested"}},
		},
		{
			name: "the current one from the listing when the service refused",
			standing: pullRequestStanding{
				state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
				number: 741,
			},
			listed: agentPullRequests{answered: true, err: errors.New("refused")},
			want:   []recordField{{"pull request", "#741 draft"}},
		},
		{
			name: "an approval waiting on its checks reads as open with the run beside it",
			standing: pullRequestStanding{
				state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
				checks: pullrequestv1.Checks_CHECKS_PENDING,
				number: 741,
			},
			want: []recordField{{"pull request", "#741 open, checks running"}},
		},
		{
			name: "a failing run names itself once",
			standing: pullRequestStanding{
				state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
				checks: pullrequestv1.Checks_CHECKS_FAILING,
				number: 741,
			},
			want: []recordField{{"pull request", "#741 checks failing"}},
		},
		{
			name: "one pull request with how long ago it merged",
			standing: pullRequestStanding{
				state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				number: 740,
			},
			listed: agentPullRequests{
				answered: true,
				items: []agentPullRequest{{
					number:   740,
					state:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					current:  true,
					openedAt: yesterday,
					mergedAt: &twoHoursAgo,
					closedAt: &twoHoursAgo,
				}},
			},
			want: []recordField{{"pull request", "#740 merged 2h ago"}},
		},
		{
			name: "several, the current one open with the rest on their own row",
			standing: pullRequestStanding{
				state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				number: 742,
			},
			listed: agentPullRequests{
				answered: true,
				items: []agentPullRequest{
					{
						number:   742,
						state:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
						current:  true,
						openedAt: twoHoursAgo,
					},
					{
						number:   711,
						state:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
						openedAt: yesterday,
						mergedAt: &yesterday,
						closedAt: &yesterday,
					},
					{
						number:   708,
						state:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
						openedAt: yesterday,
						closedAt: &yesterday,
					},
				},
			},
			want: []recordField{
				{"pull request", "#742 approved, opened 2h ago"},
				{"pull requests", "#711 merged, #708 closed"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestFields(tt.standing, tt.listed))
		})
	}
}

// TestAgentFieldsPlaceThePullRequestAfterTheBranch covers the rows landing under the branch
// they are about, and not at all on an agent whose checkout is on no branch.
func TestAgentFieldsPlaceThePullRequestAfterTheBranch(t *testing.T) {
	agent := panel.Agent{
		Name:   "worker",
		Branch: xptr.Ptr("agent/x"),
	}

	fields := agentFields(agentFieldsParams{
		agent:    agent,
		standing: pullRequestStanding{state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN, number: 7},
	})
	labels := xslice.Map(fields, func(field recordField) string {
		return field.label
	})

	branch := indexOf(labels, "branch")
	require.NotEqual(t, -1, branch)
	assert.Equal(t, "pull request", labels[branch+1])

	agent.Branch = nil
	assert.NotContains(t, agentFields(agentFieldsParams{agent: agent}), recordField{"pull request", "not read yet"})
}

func indexOf(labels []string, label string) int {
	for i, candidate := range labels {
		if candidate == label {
			return i
		}
	}

	return -1
}

// recordingPullRequests stands in for the service, answering one listing and counting the asks.
type recordingPullRequests struct {
	pullrequestv1connect.PullRequestServiceClient
	asked int
	err   error
}

func (r *recordingPullRequests) ListAgentPullRequests(context.Context, *connect.Request[pullrequestv1.ListAgentPullRequestsRequest]) (*connect.Response[pullrequestv1.ListAgentPullRequestsResponse], error) {
	r.asked++
	if r.err != nil {
		return nil, r.err
	}

	return connect.NewResponse(&pullrequestv1.ListAgentPullRequestsResponse{
		RepositoryReadAt: xptr.Ptr("2026-09-20T12:00:00Z"),
		PullRequests: []*pullrequestv1.AgentPullRequest{{
			Number:   740,
			Url:      "https://github.com/harrisoncramer/weave/pull/740",
			State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
			Current:  true,
			OpenedAt: "2026-09-19T12:00:00Z",
			MergedAt: xptr.Ptr("2026-09-20T10:00:00Z"),
		}},
	}), nil
}

// TestStageTickOnTheAgentsListAsksForThePullRequests covers the rest of the cursor being what
// sends list_agent_pull_requests, the answer landing on the record, and a rest the cursor has
// moved on from asking nothing.
func TestStageTickOnTheAgentsListAsksForThePullRequests(t *testing.T) {
	client := &recordingPullRequests{}
	m := gitModel(t)
	m.api.pullRequests = client
	m.listing.agents[0].agent.ID = "a1"
	m.listing.agents[0].agent.Branch = xptr.Ptr("agent/x")
	m.listing.agents[0].pullRequest = pullRequestStanding{state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, number: 740}

	next, cmd := m.deferStaging()
	require.NotNil(t, cmd, "a move schedules a rest")

	ask := next.applyStageTick(stageTickMsg{move: next.staging})
	require.NotNil(t, ask, "the rest asks for the hovered agent")
	assert.True(t, next.pullRequests.listed["a1"].pending)
	assert.Nil(t, next.applyStageTick(stageTickMsg{move: next.staging - 1}), "a rest the cursor moved on from asks for nothing")

	for _, msg := range produced(ask) {
		updated, _ := next.Update(msg)
		landed, ok := updated.(model)
		require.True(t, ok)
		next = landed
	}

	assert.Equal(t, 1, client.asked)
	listed := next.pullRequests.listed["a1"]
	assert.True(t, listed.answered)
	assert.False(t, listed.pending)
	require.Len(t, listed.items, 1)
	assert.Equal(t, int32(740), listed.items[0].number)
	assert.Contains(t, next.agentsDetails().fields, recordField{"pull request", "#740 merged " + ageOf(t, "2026-09-20T10:00:00Z") + " ago"})
}

// TestRefreshAsksForTheHoveredAgentsPullRequestsWhenTheyMove covers a listing landing without
// a move: the hovered agent is asked for once, not again while the answer is on its way or once
// it has come, and again once its row's standing has moved.
func TestRefreshAsksForTheHoveredAgentsPullRequestsWhenTheyMove(t *testing.T) {
	m := gitModel(t)
	m.api.pullRequests = &recordingPullRequests{}
	m.listing.agents[0].agent.ID = "a1"
	m.listing.agents[0].pullRequest = pullRequestStanding{state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN, number: 740}

	require.NotNil(t, m.listHoveredPullRequestsCmd())
	assert.True(t, m.pullRequests.listed["a1"].pending)
	assert.Nil(t, m.listHoveredPullRequestsCmd(), "an answer on its way is not asked for again")
	assert.Nil(t, m.listPullRequestsCmd(), "not even by a rest of the cursor")

	m.pullRequests.apply(agentPullRequestsListedMsg{agentID: "a1", listed: agentPullRequests{
		answered: true,
		standing: m.listing.agents[0].pullRequest,
		err:      errors.New("refused"),
	}})
	assert.Nil(t, m.listHoveredPullRequestsCmd(), "an answer, even a refusal, is not asked for again")
	assert.NotNil(t, m.listPullRequestsCmd(), "a rest of the cursor reads it afresh")

	m.pullRequests.apply(agentPullRequestsListedMsg{agentID: "a1", listed: agentPullRequests{
		answered: true,
		standing: m.listing.agents[0].pullRequest,
	}})
	m.listing.agents[0].pullRequest.state = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	assert.NotNil(t, m.listHoveredPullRequestsCmd(), "a standing that moved is read again")

	m.screen = screenWorktrees
	assert.Nil(t, m.listHoveredPullRequestsCmd())
}

// TestAgentsLeavingTheListingDropTheirPullRequests covers the answers kept only for agents the
// listing still holds.
func TestAgentsLeavingTheListingDropTheirPullRequests(t *testing.T) {
	m := gitModel(t)
	m.pullRequests.apply(agentPullRequestsListedMsg{agentID: "gone", listed: agentPullRequests{answered: true}})
	m.pullRequests.apply(agentPullRequestsListedMsg{agentID: "kept", listed: agentPullRequests{answered: true}})

	m.applyAgents([]agentItem{{agent: panel.Agent{ID: "kept"}}})

	_, gone := m.pullRequests.listed["gone"]
	_, kept := m.pullRequests.listed["kept"]
	assert.False(t, gone)
	assert.True(t, kept)
}

// ageOf is how long ago a wire stamp was, as the record spells it.
func ageOf(t *testing.T, stamp string) string {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, stamp)
	require.NoError(t, err)

	return label.Age(parsed)
}
