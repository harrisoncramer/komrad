package ui

import (
	"context"
	"errors"
	"testing"

	"claude-agents/internal/errui"
	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestAgentRows checks the tree the list renders. Every workflow is a header with
// its agents beneath it, and agents belonging to none trail under a header of
// their own.
func TestAgentRows(t *testing.T) {
	tests := []struct {
		name      string
		workflows []workflowstore.Workflow
		agents    []agentItem
		wantRows  []string
	}{
		{
			name:     "an empty store still shows the no-workflow header",
			wantRows: []string{"header:" + noWorkflowLabel},
		},
		{
			name:      "an empty workflow keeps its header, so an agent can be added to it",
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			wantRows: []string{
				"header:backend",
				"header:" + noWorkflowLabel,
			},
		},
		{
			name: "agents nest under their workflow and unassigned ones trail",
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
				{ID: "w2", Name: "frontend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", WorkflowID: ptr("w1")}},
				{agent: agentstore.Agent{Name: "loose"}},
			},
			wantRows: []string{
				"header:backend",
				"agent:auth",
				"header:frontend",
				"header:" + noWorkflowLabel,
				"agent:loose",
			},
		},
		{
			name: "an agent whose workflow is gone falls in with the unassigned",
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "orphan", WorkflowID: ptr("vanished")}},
			},
			wantRows: []string{
				"header:" + noWorkflowLabel,
				"agent:orphan",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{workflows: tt.workflows, agents: tt.agents}

			assert.Equal(t, tt.wantRows, describeRows(m.agentRows()))
		})
	}
}

// TestHoveredWorkflow checks the cursor reports a real workflow only when it
// sits on that workflow's header, since the workflow keys act on what is hovered.
func TestHoveredWorkflow(t *testing.T) {
	tests := []struct {
		name    string
		cursor  int
		wantIdx int
		wantOK  bool
	}{
		{
			name:   "on an agent",
			cursor: 1,
		},
		{
			name:   "on the no-workflow header, which has no record behind it",
			cursor: 2,
		},
		{
			name:   "on an agent belonging to no workflow",
			cursor: 3,
		},
		{
			name:    "on a workflow header",
			cursor:  0,
			wantIdx: 0,
			wantOK:  true,
		},
	}

	// rows: 0 backend header, 1 auth, 2 no-workflow header, 3 loose.
	m := model{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
		agents: []agentItem{
			{agent: agentstore.Agent{Name: "auth", WorkflowID: ptr("w1")}},
			{agent: agentstore.Agent{Name: "loose"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.agentCursor = tt.cursor

			idx, ok := m.hoveredWorkflow()
			require.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantIdx, idx)
			}
		})
	}
}

// refusingWorkflows refuses every call and counts the writes it was asked for, so a
// test can tell a call the panel decided not to make from one the service turned
// down. The embedded interface is nil, so any other method panics rather than
// quietly returning a zero value.
type refusingWorkflows struct {
	workflowv1connect.WorkflowServiceClient
	err    error
	writes int
}

func (r *refusingWorkflows) ListWorkflows(context.Context, *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	return nil, r.err
}

func (r *refusingWorkflows) CreateWorkflow(context.Context, *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	r.writes++

	return nil, r.err
}

func (r *refusingWorkflows) UpdateWorkflow(context.Context, *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	r.writes++

	return nil, r.err
}

func (r *refusingWorkflows) DeleteWorkflow(context.Context, *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	r.writes++

	return nil, r.err
}

// TestWorkflowWrites covers each write handing its error back rather than
// discarding it, which is what leaves the form that called it something to report.
// The last case is the one write the panel turns down itself: a blank name is a form
// submitted empty, not a failure, so the service is never asked.
func TestWorkflowWrites(t *testing.T) {
	tests := []struct {
		name       string
		call       func(model) error
		wantWrites int
		wantReason string
	}{
		{
			name:       "a refused create",
			call:       func(m model) error { return m.createWorkflow(workflowstore.Config{Name: "api"}) },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused update",
			call:       func(m model) error { return m.updateWorkflow(workflowstore.Workflow{ID: "w1", Name: "api"}) },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused delete",
			call:       func(m model) error { return m.deleteWorkflow("w1", true) },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name: "an update to a blank name never reaches the service",
			call: func(m model) error { return m.updateWorkflow(workflowstore.Workflow{ID: "w1", Name: "  "}) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflows := &refusingWorkflows{err: connect.NewError(connect.CodeResourceExhausted, errors.New("quota reached"))}
			m := model{api: api{workflows: workflows}}

			err := tt.call(m)

			assert.Equal(t, tt.wantWrites, workflows.writes)
			if tt.wantReason == "" {
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Equal(t, tt.wantReason, errui.Reason(err), "the service's message is what a banner would carry")
		})
	}
}

// TestFailedWorkflowListKeepsTheTree covers a workflow list that does not answer.
// The tree stays as it was, since blanking it would say every workflow had just been
// deleted.
func TestFailedWorkflowListKeepsTheTree(t *testing.T) {
	m := model{
		api:            api{workflows: &refusingWorkflows{err: errors.New("connection reset")}},
		workflows:      []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
		workflowCursor: 1,
	}

	m.reloadWorkflows()

	assert.Len(t, m.workflows, 2, "the tree already on screen is kept")
	assert.Equal(t, 1, m.workflowCursor, "and the cursor stays where the user left it")
	require.NotNil(t, m.actionErr)
	assert.Equal(t, "Could not list workflows", m.actionErr.Summary)
}

// describeRows flattens the tree into strings, so a case states the shape it
// expects rather than asserting field by field.
func describeRows(rows []agentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.header {
			out = append(out, "header:"+row.label)
			continue
		}

		out = append(out, "agent:"+row.item.agent.Name)
	}

	return out
}

func ptr(s string) *string { return &s }
