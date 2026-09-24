package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/config"
	"weave/internal/client"
	"weave/internal/engine"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range produced(cmd) {
		switch msg.(type) {
		case writeResult, client.WatchBatch:
		default:
			continue
		}

		updated, _ := m.Update(msg)
		next, ok := updated.(model)
		require.True(t, ok)
		m = next
	}

	return m
}

// applyListing hands the model the live workflows the way a watch snapshot does, with no agents.
func applyListing(m *model, workflows []panel.Workflow) {
	m.withListing(func(l *listing) {
		l.agents, l.workflows = nil, workflows
	})
}

func produced(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, batched := range msg {
			msgs = append(msgs, produced(batched)...)
		}

		return msgs
	default:
		return []tea.Msg{msg}
	}
}

// TestWriteLandsOnceTheWatchDeliversTheRow covers a create landing: the world waits for the
// watch to carry the row the write made, and the cursor goes onto it when it does.
func TestWriteLandsOnceTheWatchDeliversTheRow(t *testing.T) {
	m := landingPanel()

	landed, _ := m.applyWrite(writeResult{written: &written{row: agentRowKey("a2")}})
	got := asModel(t, landed)

	_, held := got.listing.liveAgent("a2")
	require.False(t, held, "the write puts nothing into the world")
	assert.Equal(t, agentRowKey("a1"), hoveredAgentRowKey(got), "and the cursor waits where it was")

	got.applyWatch(client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{
		Id:         "a2",
		Name:       "new",
		WorkflowId: "w1",
		Alive:      proto.Bool(true),
	}))})

	assert.Equal(t, agentRowKey("a2"), hoveredAgentRowKey(got), "the event that carries the row takes the cursor to it")
	assert.Empty(t, got.landing.row)
}

// TestWriteLandsOnceTheWatchDeliversTheWorkflow covers a workflow create, whose header the
// cursor goes to once the watch carries the workflow.
func TestWriteLandsOnceTheWatchDeliversTheWorkflow(t *testing.T) {
	m := landingPanel()

	landed, _ := m.applyWrite(writeResult{written: &written{row: workflowRowKey("w2")}})
	got := asModel(t, landed)
	require.Len(t, got.listing.workflows, 1)

	got.applyWatch(client.WatchBatch{putEvent(workflowRowOf(&workflowv1.Workflow{Id: "w2", Name: "api"}))})

	require.Len(t, got.listing.workflows, 2)
	assert.Equal(t, workflowRowKey("w2"), hoveredAgentRowKey(got))
}

// TestWriteLandsOnARowTheWatchAlreadyDelivered covers the write's answer arriving after the
// watch has already put the row on screen.
func TestWriteLandsOnARowTheWatchAlreadyDelivered(t *testing.T) {
	m := landingPanel()
	m.applyWatch(client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{Id: "a2", Name: "new", WorkflowId: "w1"}))})

	landed, _ := m.applyWrite(writeResult{written: &written{row: agentRowKey("a2")}})
	got := asModel(t, landed)

	assert.Equal(t, agentRowKey("a2"), hoveredAgentRowKey(got))
	assert.Empty(t, got.landing.row, "and nothing is left waiting")
}

// TestLandingNeverWaitsForever covers the row a landing waits on not turning up while it waits,
// so a later listing leaves the cursor alone.
func TestLandingNeverWaitsForever(t *testing.T) {
	arrived := putEvent(agentRowOf(&agentv1.Agent{Id: "a2", Name: "new", WorkflowId: "w1"}))

	tests := []struct {
		name  string
		write func(m *model) model
		want  string
	}{
		{
			name: "a failed create holds nothing",
			write: func(m *model) model {
				landed, _ := m.applyWrite(writeResult{summary: "Could not create the agent", err: errors.New("refused")})

				return asModel(t, landed)
			},
			want: agentRowKey("a1"),
		},
		{
			name: "a row that never arrives gives up once the wait runs out",
			write: func(m *model) model {
				m.landOn(written{row: agentRowKey("a2")}, time.Now().Add(-landingWait-time.Second))

				return *m
			},
			want: agentRowKey("a1"),
		},
		{
			name: "a key pressed while waiting takes the cursor back",
			write: func(m *model) model {
				m.landOn(written{row: agentRowKey("a2")}, time.Now())

				return pressMark(t, *m, "k")
			},
			want: workflowRowKey("w1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := landingPanel()
			got := tt.write(&m)

			got.applyWatch(client.WatchBatch{arrived})

			assert.Equal(t, tt.want, hoveredAgentRowKey(got))
			assert.Empty(t, got.landing.row)
		})
	}
}

// TestLandingWaitsOnARowAFilterHides covers the watch delivering the row while the listing's
// filter keeps it off the screen, which leaves the cursor where it was.
func TestLandingWaitsOnARowAFilterHides(t *testing.T) {
	m := landingPanel()
	m.filter.mode = filterModeFilter
	m.filter.setQuery("^old$")
	m.landOn(written{row: agentRowKey("a2")}, time.Now())

	m.applyWatch(client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{Id: "a2", Name: "new", WorkflowId: "w1"}))})

	_, held := m.listing.liveAgent("a2")
	require.True(t, held, "the row is in the world")
	assert.Equal(t, agentRowKey("a1"), hoveredAgentRowKey(m), "and the cursor stays on a row it can see")
	assert.Equal(t, agentRowKey("a2"), m.landing.row, "until the wait runs out")
}

// TestCreatedAgentReceivesFocus covers the cursor landing on a created agent and staying there
// once the watch delivers its row.
func TestCreatedAgentReceivesFocus(t *testing.T) {
	agents := &formRecorder{}
	tmux := &recordingTmux{}
	m := model{
		api:    api{agents: agents, hooks: agents},
		tmux:   tmux,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		agents: agentsScreen{rowCursor: rowCursor{cursor: 1}, placed: true},
	}
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:         "old",
			Name:       "api-refactor",
			WorkflowId: "w1",
		}),
	)})

	workflow := m.listing.workflows[0]

	values := agentPrefill(workflow)
	values.Title = "api-tests"
	values.Dir = "/work/api"

	opened := m.openAgentForm(workflow, &values)
	m = asModel(t, opened)

	submitted, cmd := m.answerForm()
	m = asModel(t, submitted)

	for _, msg := range produced(cmd) {
		if result, ok := msg.(writeResult); ok {
			updated, _ := m.applyWrite(result)
			m = asModel(t, updated)
		}
	}

	m.applyWatch(client.WatchBatch{
		putEvent(agentRowOf(&agentv1.Agent{
			Id:         "new",
			Name:       "api-tests",
			WorkflowId: "w1",
		})),
	})

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "new", selected.Agent.ID)
}

// TestUnrelatedWriteLeavesTheInfoPaneAlone covers a rename or an archive landing while a log
// pane is up: that write has nothing to do with the pane, so it must not tear it down.
func TestUnrelatedWriteLeavesTheInfoPaneAlone(t *testing.T) {
	tmux := &recordingTmux{}
	m := model{
		tmux:   tmux,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		info:   infoState{kind: infoRunLog},
	}

	updated, _ := m.applyWrite(writeResult{summary: "Could not rename the agent"})
	got := asModel(t, updated)

	assert.Equal(t, infoRunLog, got.info.kind, "a write that never touched the pane leaves it alone")
}

// TestCreatedWorkflowReceivesFocus covers the same for a folder, where the cursor lands on
// the new header rather than staying where the form opened.
func TestCreatedWorkflowReceivesFocus(t *testing.T) {
	recorder := &formRecorder{}
	m := model{
		api: api{
			agents:    recorder,
			workflows: recorder,
			hooks:     recorder,
		},
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []panel.Workflow{{ID: "w1", Name: "api"}}}, agents: agentsScreen{placed: true},
	}

	values := workflowFormValues{Name: "web"}

	opened := m.openWorkflowConfigForm(&values, nil)
	m = asModel(t, opened)

	submitted, cmd := m.answerForm()
	m = asModel(t, submitted)

	for _, msg := range produced(cmd) {
		if result, ok := msg.(writeResult); ok {
			updated, _ := m.applyWrite(result)
			m = asModel(t, updated)
		}
	}

	applyListing(&m, []panel.Workflow{
		{ID: "w1", Name: "api"},
		{ID: "new", Name: "web"},
	})

	hovered, ok := m.agents.hoveredWorkflow(m.shared())
	require.True(t, ok, "the cursor is on a folder header")
	assert.Equal(t, "new", hovered.ID)
}

// TestArchiveCursorHoldsItsAgentWhenAFolderGoes covers the same shape on the archive, whose
// folders are replaced before its own listings land.
func TestArchiveCursorHoldsItsAgentWhenAFolderGoes(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenArchive,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}},
			archivedAgents: archivedItems([]panel.Agent{
				{
					ID:         "x1",
					Name:       "api-spike",
					WorkflowID: "w1",
				},
				{
					ID:         "x2",
					Name:       "infra-audit",
					WorkflowID: "w2",
				},
				{
					ID:         "x3",
					Name:       "infra-upgrade",
					WorkflowID: "w2",
				},
			}),
		},
		archive: archiveScreen{rowCursor: rowCursor{cursor: 3}},
	}

	applyListing(&m, []panel.Workflow{{ID: "w2", Name: "infra"}})
	m.withListing(func(l *listing) {
		l.archivedAgents = archivedItems([]panel.Agent{
			{
				ID:         "x2",
				Name:       "infra-audit",
				WorkflowID: "w2",
			},
			{
				ID:         "x3",
				Name:       "infra-upgrade",
				WorkflowID: "w2",
			},
		})
	})

	selected, ok := m.archive.hoveredAgent(m.shared())
	require.True(t, ok, "the cursor is on an archived agent")
	assert.Equal(t, "infra-audit", selected.Name)
}

type emptyLists struct {
	agentv1connect.AgentServiceClient
	workflowv1connect.WorkflowServiceClient
	schedulev1connect.ScheduleServiceClient
	worktreev1connect.WorktreeServiceClient
}

func (emptyLists) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

func (emptyLists) ListWorkflows(context.Context, *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	return connect.NewResponse(&workflowv1.ListWorkflowsResponse{}), nil
}

func (emptyLists) ListSchedules(context.Context, *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	return connect.NewResponse(&schedulev1.ListSchedulesResponse{}), nil
}

func (emptyLists) ListWorktrees(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[worktreev1.ListWorktreesResponse], error) {
	return connect.NewResponse(&worktreev1.ListWorktreesResponse{}), nil
}

func (emptyLists) GetWorktree(_ context.Context, req *connect.Request[worktreev1.GetWorktreeRequest]) (*connect.Response[worktreev1.GetWorktreeResponse], error) {
	return nil, connect.NewError(connect.CodeNotFound, errors.New(req.Msg.GetDir()+" is not under the worktree directories"))
}

// TestUnreachableHintNamesWhatCanBeDone covers the hint under a server that stopped answering
// telling a user with a remote engine about the tunnel rather than about starting a server here.
func TestUnreachableHintNamesWhatCanBeDone(t *testing.T) {
	tests := []struct {
		name   string
		remote engine.Remote
		want   string
	}{
		{
			name:   "the engine is this machine",
			remote: engine.Remote{},
			want:   "Start it with: weave serve",
		},
		{
			name:   "the engine is elsewhere",
			remote: engine.Remote{Destination: "weave@host"},
			want:   "The tunnel to the engine is dialled again until it answers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{}
			m := infoModel(tmux)
			m.frame = layout.New(tmux, config.DefaultLayout(), layout.WithEngine(tt.remote))

			m.reportUnreachable(errors.New("dial tcp 127.0.0.1:19042: connect: connection refused"))

			require.NotEmpty(t, m.messages.items)
			assert.Equal(t, tt.want, m.messages.items[0].hint)
		})
	}
}

func landingPanel() model {
	m := watchedPanel()
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{Id: "a1", Name: "old", WorkflowId: "w1"}),
	)})

	return m
}

func hoveredAgentRowKey(m model) string {
	return rowKeyAt(m.agents.rows(m.shared()), m.agents.cursor)
}

// TestHeldLandingKeepsThePaneTheWriteShowed covers a batch for another row arriving while the
// landing waits, which leaves the created pane on the stage.
func TestHeldLandingKeepsThePaneTheWriteShowed(t *testing.T) {
	tmux := &recordingTmux{}
	m := watchedPanel()
	m.tmux = tmux
	m.frame = layout.New(tmux, config.DefaultLayout())
	m.frame.Attach("%0")
	old := &agentv1.Agent{Id: "a1", Name: "old", WorkflowId: "w1", PaneId: "%1", Alive: proto.Bool(true)}
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(old),
	)})
	m.stageSelection()
	require.Equal(t, "%1", m.frame.GuestPane())

	landed, _ := m.applyWrite(writeResult{written: &written{row: agentRowKey("a2"), pane: "%9"}})
	got := asModel(t, landed)
	require.Equal(t, "%9", got.frame.GuestPane())

	got.applyWatch(client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{Id: "a1", Name: "renamed", WorkflowId: "w1", PaneId: "%1", Alive: proto.Bool(true)}))})

	assert.Equal(t, "%9", got.frame.GuestPane(), "the old row's pane is not swapped back over the new one")

	got.applyWatch(client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{Id: "a2", Name: "new", WorkflowId: "w1", PaneId: "%9", Alive: proto.Bool(true)}))})

	assert.Equal(t, agentRowKey("a2"), hoveredAgentRowKey(got))
	assert.Equal(t, "%9", got.frame.GuestPane())
}
