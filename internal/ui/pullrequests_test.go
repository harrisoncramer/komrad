package ui

import (
	"context"
	"testing"
	"time"

	"weave/internal/client"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/lib/xptr"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullRequestWords covers the state word with what is still in the way beside it.
func TestPullRequestWords(t *testing.T) {
	tests := []struct {
		name         string
		state        pullrequestv1.PullRequestState
		checks       pullrequestv1.Checks
		mergeability pullrequestv1.Mergeability
		want         string
	}{
		{
			name:   "an open row with its checks running says so",
			state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
			checks: pullrequestv1.Checks_CHECKS_PENDING,
			want:   "open, checks running",
		},
		{
			name:         "an approval waiting on its checks",
			state:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
			checks:       pullrequestv1.Checks_CHECKS_PENDING,
			mergeability: pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE,
			want:         "approved, checks running",
		},
		{
			name:         "an approval behind its base",
			state:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
			checks:       pullrequestv1.Checks_CHECKS_PASSING,
			mergeability: pullrequestv1.Mergeability_MERGEABILITY_BEHIND,
			want:         "approved, behind the base",
		},
		{
			name:         "an approval the forge has not computed yet says so",
			state:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
			checks:       pullrequestv1.Checks_CHECKS_PASSING,
			mergeability: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
			want:         "approved, forge still computing",
		},
		{
			name:         "a conflicting row says so and nothing about its checks",
			state:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
			checks:       pullrequestv1.Checks_CHECKS_PENDING,
			mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
			want:         "conflicts with the base",
		},
		{
			name:         "a draft conflicting with its base keeps the draft word and says so",
			state:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
			checks:       pullrequestv1.Checks_CHECKS_PENDING,
			mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
			want:         "draft, conflicts with the base",
		},
		{
			name:         "a branch the forge would merge says nothing more",
			state:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
			checks:       pullrequestv1.Checks_CHECKS_PASSING,
			mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			want:         "ready to merge",
		},
		{
			name:   "a failing run names itself once",
			state:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
			checks: pullrequestv1.Checks_CHECKS_FAILING,
			want:   "checks failing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestWords(tt.state, tt.checks, tt.mergeability))
		})
	}
}

// TestPullRequestFields covers the record's rows for the branch's pull requests: what the two
// blank standings say, the current one with how long ago it got where it is, and the rest
// folded into one row.
func TestPullRequestFields(t *testing.T) {
	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	yesterday := now.Add(-26 * time.Hour)

	tests := []struct {
		name         string
		standing     panel.PullRequestStanding
		pullRequests []panel.PullRequest
		want         []recordField
	}{
		{
			name:     "a repository not read yet",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD},
			want:     []recordField{{"pull request", "not read yet"}},
		},
		{
			name:     "a branch with none",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE},
			want:     []recordField{{"pull request", "none from this branch"}},
		},
		{
			name:     "one pull request with how long ago it merged",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, Number: 740},
			pullRequests: []panel.PullRequest{{
				Number:   740,
				State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				OpenedAt: yesterday,
				MergedAt: &twoHoursAgo,
				ClosedAt: &twoHoursAgo,
			}},
			want: []recordField{{"pull request", "#740 merged 2h ago"}},
		},
		{
			name:     "none since a merged one the branch has moved past",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE},
			pullRequests: []panel.PullRequest{
				{
					Number:   740,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &twoHoursAgo,
					ClosedAt: &twoHoursAgo,
				},
				{
					Number:   711,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
					OpenedAt: yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []recordField{
				{"pull request", "none since #740 merged 2h ago"},
				{"pull requests", "#711 closed"},
			},
		},
		{
			name:     "one closed with how long ago it closed",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED, Number: 740},
			pullRequests: []panel.PullRequest{{
				Number:   740,
				State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
				OpenedAt: yesterday,
				ClosedAt: &twoHoursAgo,
			}},
			want: []recordField{{"pull request", "#740 closed 2h ago"}},
		},
		{
			name:     "several, the current one open with the rest on their own row",
			standing: panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED, Number: 742},
			pullRequests: []panel.PullRequest{
				{
					Number:   742,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
					OpenedAt: twoHoursAgo,
				},
				{
					Number:   711,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
				{
					Number:   708,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
					OpenedAt: yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []recordField{
				{"pull request", "#742 ready to merge, opened 2h ago"},
				{"pull requests", "#711 merged, #708 closed"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestFields(tt.standing, tt.pullRequests))
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
		agent:        agent,
		standing:     panel.PullRequestStanding{State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN, Number: 7},
		pullRequests: []panel.PullRequest{{Number: 7, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN}},
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

// recordingPullRequests stands in for the service, answering one merged pull request on
// whatever branch is asked for and keeping the branches asked.
type recordingPullRequests struct {
	pullrequestv1connect.PullRequestServiceClient
	asked []string
	err   error
}

func (r *recordingPullRequests) ListPullRequests(_ context.Context, req *connect.Request[pullrequestv1.ListPullRequestsRequest]) (*connect.Response[pullrequestv1.ListPullRequestsResponse], error) {
	r.asked = append(r.asked, req.Msg.GetRepositoryUrl()+" "+req.Msg.GetHeadBranch())
	if r.err != nil {
		return nil, r.err
	}

	return connect.NewResponse(&pullrequestv1.ListPullRequestsResponse{
		PullRequests: []*pullrequestv1.PullRequest{{
			Id:                "pr-740",
			RepositoryUrl:     req.Msg.GetRepositoryUrl(),
			HeadRepositoryUrl: req.Msg.GetRepositoryUrl(),
			HeadBranch:        req.Msg.GetHeadBranch(),
			Number:            740,
			Url:               "https://github.com/harrisoncramer/weave/pull/740",
			State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
			OpenedAt:          "2026-09-19T12:00:00Z",
			MergedAt:          xptr.Ptr("2026-09-20T10:00:00Z"),
			ClosedAt:          xptr.Ptr("2026-09-20T10:00:00Z"),
		}},
	}), nil
}

// TestArrivedAgentsListTheirPullRequests covers the one unary call the panel makes for pull
// requests: for an agent the world had not seen, or one whose branch moved, and never for a
// snapshot or an event that leaves the branch where it was.
func TestArrivedAgentsListTheirPullRequests(t *testing.T) {
	forge := &recordingPullRequests{}
	m := watchedPanel()
	m.api.pullRequests = forge
	m.width, m.height = 200, 40

	m, cmd := update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:            "a1",
			WorkflowId:    "w1",
			RepositoryUrl: xptr.Ptr(repository),
			Branch:        xptr.Ptr("agent/x"),
		}),
	)})
	m = settle(t, m, cmd)
	assert.Empty(t, forge.asked, "the snapshot carries the pull requests of the agents it carries")

	m, cmd = update(t, m, client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:            "a2",
			WorkflowId:    "w1",
			RepositoryUrl: xptr.Ptr(repository),
			Branch:        xptr.Ptr("agent/y"),
		})),
	})
	m = settle(t, m, cmd)
	require.Equal(t, []string{repository + " agent/y"}, forge.asked, "an agent the world had not seen is asked for")

	row, ok := m.listing.liveAgent("a2")
	require.True(t, ok)
	assert.Equal(t, int32(740), row.PullRequest.Number, "and what came back draws on its row")
	m.agents.cursor = rowIndexOfAgentID(m.agents.rows(m.shared()), "a2")
	assert.Contains(t, m.agents.details(m.shared()).fields, recordField{"pull request", "#740 merged " + ageOf(t, "2026-09-20T10:00:00Z") + " ago"})

	m, cmd = update(t, m, client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:            "a2",
			Name:          "renamed",
			WorkflowId:    "w1",
			RepositoryUrl: xptr.Ptr(repository),
			Branch:        xptr.Ptr("agent/y"),
		})),
	})
	m = settle(t, m, cmd)
	assert.Len(t, forge.asked, 1, "an event that leaves the branch where it was asks for nothing")

	m, cmd = update(t, m, client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:            "a2",
			WorkflowId:    "w1",
			RepositoryUrl: xptr.Ptr(repository),
			Branch:        xptr.Ptr("agent/z"),
		})),
	})
	settle(t, m, cmd)
	assert.Equal(t, []string{repository + " agent/y", repository + " agent/z"}, forge.asked, "a branch that moved is asked for again")
}

// TestPullRequestOnNobodysBranchIsHeldAndNotDrawn covers a pull request arriving for a branch
// no held agent is on: it draws on no row, and draws on the agent that arrives on that branch
// afterwards.
func TestPullRequestOnNobodysBranchIsHeldAndNotDrawn(t *testing.T) {
	m := watchedPanel()
	m.api.pullRequests = &recordingPullRequests{}

	m, _ = update(t, m, client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:             "a1",
			WorkflowId:     "w1",
			RepositoryUrl:  xptr.Ptr(repository),
			Branch:         xptr.Ptr("agent/x"),
			RepositoryRead: true,
		}),
	)})

	m, _ = update(t, m, client.WatchBatch{putEvent(pullRequestRowOf("pr-1", "agent/other", 7))})

	require.Len(t, m.listing.agents, 1)
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE, m.listing.agents[0].PullRequest.State, "the row on another branch is untouched")
	assert.Len(t, m.listing.world.PullRequests, 1, "the pull request is held for the agent that may arrive")

	m, _ = update(t, m, client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:            "a2",
			WorkflowId:    "w1",
			RepositoryUrl: xptr.Ptr(repository),
			Branch:        xptr.Ptr("agent/other"),
		})),
	})

	row, ok := m.listing.liveAgent("a2")
	require.True(t, ok)
	assert.Equal(t, int32(7), row.PullRequest.Number, "and draws on it once it has")
}

const repository = "https://github.com/harrisoncramer/weave"

func pullRequestRowOf(id, branch string, number int32) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: &pullrequestv1.PullRequest{
		Id:                id,
		RepositoryUrl:     repository,
		HeadRepositoryUrl: repository,
		HeadBranch:        branch,
		Number:            number,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		OpenedAt:          "2026-09-19T12:00:00Z",
	}}}
}

// ageOf is how long ago a wire stamp was, as the record spells it.
func ageOf(t *testing.T, stamp string) string {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, stamp)
	require.NoError(t, err)

	return label.Age(parsed)
}
