package ui

import (
	"testing"

	"weave/internal/client"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestPullRequestReactorAsksOnArrival covers which agents already held are asked for again: one
// back from the archive or one dropped and put again, and never one being archived or on no branch.
func TestPullRequestReactorAsksOnArrival(t *testing.T) {
	archivedAt := "2026-09-23T10:00:00Z"
	live := &agentv1.Agent{Id: "a1", WorkflowId: "w1", RepositoryUrl: xptr.Ptr(repository), Branch: xptr.Ptr("agent/x")}
	archived := &agentv1.Agent{Id: "a1", WorkflowId: "w1", RepositoryUrl: xptr.Ptr(repository), Branch: xptr.Ptr("agent/x"), ArchivedAt: &archivedAt}
	branchless := &agentv1.Agent{Id: "a1", WorkflowId: "w1", RepositoryUrl: xptr.Ptr(repository)}

	tests := []struct {
		name    string
		held    *agentv1.Agent
		batches []client.WatchBatch
		want    []string
	}{
		{
			name:    "an agent being archived is not asked for",
			held:    live,
			batches: []client.WatchBatch{{putEvent(agentRowOf(archived))}},
			want:    nil,
		},
		{
			name:    "an agent on no branch is not asked for",
			held:    branchless,
			batches: []client.WatchBatch{{putEvent(agentRowOf(branchless))}},
			want:    nil,
		},
		{
			name:    "an agent back from the archive is asked for",
			held:    archived,
			batches: []client.WatchBatch{{putEvent(agentRowOf(live))}},
			want:    []string{repository + " agent/x"},
		},
		{
			name: "an agent dropped and put again is asked for",
			held: live,
			batches: []client.WatchBatch{
				{deletedEvent(watchv1.Resource_RESOURCE_AGENT, "a1")},
				{putEvent(agentRowOf(live))},
			},
			want: []string{repository + " agent/x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forge := &recordingPullRequests{}
			m := watchedPanel()
			m.api.pullRequests = forge
			m.width, m.height = 200, 40

			m, cmd := update(t, m, client.WatchBatch{snapshotEvent(
				workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
				agentRowOf(tt.held),
			)})
			m = settle(t, m, cmd)
			for _, batch := range tt.batches {
				m, cmd = update(t, m, batch)
				m = settle(t, m, cmd)
			}

			assert.Equal(t, tt.want, forge.asked)
		})
	}
}
