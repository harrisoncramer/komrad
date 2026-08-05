package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// / TestAgentRows checks the tree the list renders. Every workflow is a header
// with its agents beneath it, and every agent has a workflow, so nothing trails
// underneath outside one.
func TestAgentRows(t *testing.T) {
	tests := []struct {
		name      string
		workflows []workflowstore.Workflow
		agents    []agentItem
		wantRows  []string
	}{
		{
			name:     "an empty store renders nothing at all",
			wantRows: []string{},
		},
		{
			name:      "an empty workflow keeps its header, so an agent can be added to it",
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			wantRows:  []string{"header:backend"},
		},
		{
			name: "agents nest under the workflow they belong to",
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
				{ID: "w2", Name: "frontend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", WorkflowID: "w1"}},
				{agent: agentstore.Agent{Name: "ui", WorkflowID: "w2"}},
			},
			wantRows: []string{
				"header:backend",
				"agent:auth",
				"header:frontend",
				"agent:ui",
			},
		},
		{
			// The workflow was deleted and the agent still points at it. There is no
			// header to nest it under, so it is left out rather than rendered loose.
			name: "an agent whose workflow is gone is not rendered",
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", WorkflowID: "w1"}},
				{agent: agentstore.Agent{Name: "orphan", WorkflowID: "vanished"}},
			},
			wantRows: []string{
				"header:backend",
				"agent:auth",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{tree: treeState{workflows: tt.workflows, agents: tt.agents}}

			assert.Equal(t, tt.wantRows, describeRows(m.agentRows()))
		})
	}
}

// TestHoveredWorkflow checks the cursor reports a real workflow only when it
// sits on that workflow's header, since the workflow keys act on what is hovered.
func TestHoveredWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		cursor   int
		wantName string
		wantOK   bool
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
			name:     "on a workflow header",
			cursor:   0,
			wantName: "backend",
			wantOK:   true,
		},
	}

	// rows: 0 backend header, 1 auth, 2 no-workflow header, 3 loose.
	m := model{
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", WorkflowID: "w1"}},
				{agent: agentstore.Agent{Name: "loose"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.tree.cursor = tt.cursor

			workflow, ok := m.hoveredWorkflow()
			require.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantName, workflow.Name)
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

func (r *refusingWorkflows) ArchiveWorkflow(context.Context, *connect.Request[workflowv1.ArchiveWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
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
			name: "a refused create",
			call: func(m model) error {
				_, err := m.api.createWorkflow(workflowstore.Config{Name: "api"})

				return err
			},
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused update",
			call:       func(m model) error { return m.api.updateWorkflow(workflowstore.Workflow{ID: "w1", Name: "api"}) },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused archive",
			call:       func(m model) error { return m.api.archiveWorkflow("w1") },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name: "an update to a blank name never reaches the service",
			call: func(m model) error { return m.api.updateWorkflow(workflowstore.Workflow{ID: "w1", Name: "  "}) },
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

// TestAvailableActionsSkipUnderscoredNames covers what the workflow form's setup and cleanup
// fields are chosen from. A workflow naming a file that only exists to be sourced would run a
// fragment on its own, so the picker offers the same list a person sees on the actions screen.
//
// What each file declares comes with it, so choosing a setup action means reading what the
// scripts do rather than guessing from filenames.
func TestAvailableActionsSkipUnderscoredNames(t *testing.T) {
	m := model{api: api{actions: &fakeActions{}}}

	got := m.getAvailableActions()

	assert.Equal(t, []actionChoice{
		{name: "open-pr"},
		{name: "unrunnable"},
		{name: "compiled"},
	}, got)
}

// TestFailedWorkflowListKeepsTheTree covers a workflow list that does not answer.
// The tree stays as it was, since blanking it would say every workflow had just been
// archived.
func TestFailedWorkflowListKeepsTheTree(t *testing.T) {
	m := model{
		stage: NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
		},
	}

	m.applyRefresh(refreshResult{workflowsErr: errors.New("connection reset")})

	assert.Len(t, m.tree.workflows, 2, "the tree already on screen is kept")
	assert.Error(t, m.unreachable, "and the panel says the server is not answering")
}

// TestApplyWorkflows covers what the refresh does to the tree's folders. The
// listing comes back newest first, so the fresh values are taken as they come.
func TestApplyWorkflows(t *testing.T) {
	tests := []struct {
		name      string
		current   []workflowstore.Workflow
		fresh     []workflowstore.Workflow
		wantNames []string
	}{
		{
			name:      "an edit lands on the workflow without moving it",
			current:   []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
			fresh:     []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web", Context: "notes"}},
			wantNames: []string{"api", "web"},
		},
		{
			name:      "a workflow created elsewhere lands where the listing put it",
			current:   []workflowstore.Workflow{{ID: "w1", Name: "api"}},
			fresh:     []workflowstore.Workflow{{ID: "w2", Name: "web"}, {ID: "w1", Name: "api"}},
			wantNames: []string{"web", "api"},
		},
		{
			name:      "a workflow archived elsewhere drops out",
			current:   []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
			fresh:     []workflowstore.Workflow{{ID: "w2", Name: "web"}},
			wantNames: []string{"web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{tree: treeState{workflows: tt.current}}

			m.applyWorkflows(tt.fresh)

			names := make([]string, 0, len(m.tree.workflows))
			for _, w := range m.tree.workflows {
				names = append(names, w.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// TestRefreshRestagesWorkflowContext covers the reason the refresh carries
// workflows at all. A context edited through the MCP tools or another panel only
// reaches this one through the listing, and the stage has to redraw for it to be
// seen. Restaging identical text would respawn the pane every tick, so that is
// checked not to happen.
func TestRefreshRestagesWorkflowContext(t *testing.T) {
	tmux := &recordingTmux{}
	stage := NewStage(tmux, config.DefaultLayout())
	stage.Init("%0")

	// The cursor sits on the workflow's header, which is row 0.
	m := model{
		stage:  stage,
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api", Context: "old notes"}},
			cursor:    0,
		},
	}
	m.stageSelection()
	staged := len(tmux.respawns)

	fresh := refreshResult{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api", Context: "new notes"}}}
	updated, _ := m.Update(fresh)
	m, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, "new notes", m.tree.workflows[0].Context)
	require.Greater(t, len(tmux.respawns), staged, "the edit is restaged without the cursor moving")

	shown, err := os.ReadFile(filepath.Join(os.TempDir(), "weave-stage.ansi"))
	require.NoError(t, err)
	// The file holds the rendered document, where the colours around a word sit
	// between it and the next, so the text is read back without them.
	assert.Contains(t, ansi.Strip(string(shown)), "new notes")

	staged = len(tmux.respawns)
	updated, _ = m.Update(fresh)
	_, ok = updated.(model)
	require.True(t, ok)

	assert.Equal(t, staged, len(tmux.respawns), "an unchanged refresh leaves the stage pane alone")
}

// describeRows flattens the tree into strings, so a case states the shape it
// expects rather than asserting field by field.
func describeRows(rows []agentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.header {
			out = append(out, "header:"+row.workflow.Name)
			continue
		}

		out = append(out, "agent:"+row.item.agent.Name)
	}

	return out
}
