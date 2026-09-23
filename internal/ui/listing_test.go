package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	"weave/internal/client"
	"weave/internal/engine"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range produced(cmd) {
		switch msg.(type) {
		case writeResult, listedMsg, client.WatchBatch, pullRequestsListedMsg:
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

// applyListing hands the model the three live listings the way a watch snapshot does.
func applyListing(m *model, agents []panel.AgentRow, workflows []panel.Workflow, schedules []scheduleItem) tea.Cmd {
	return m.withListing(func(l *listing) {
		l.agents, l.workflows, l.schedules = agents, workflows, schedules
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

// TestWriteLandsOnTheRowItMade covers a create landing: the row the write answered with goes
// into the listing before the watch has delivered it, and the cursor onto it.
func TestWriteLandsOnTheRowItMade(t *testing.T) {
	m := watchedPanel()
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:         "a1",
			Name:       "old",
			WorkflowId: "w1",
		}),
	)})

	landed, _ := m.applyWrite(writeResult{written: &written{
		row: agentRowKey("a2"),
		agent: &agentv1.Agent{
			Id:         "a2",
			Name:       "new",
			WorkflowId: "w1",
		},
	}})
	got := asModel(t, landed)

	_, held := got.listing.liveAgent("a2")
	require.True(t, held, "the row is on screen the moment the write lands")
	selected, ok := got.agents.selectedAgent(got.shared())
	require.True(t, ok)
	assert.Equal(t, "new", selected.Agent.Name, "and the cursor is on it")

	got.applyWatch(client.WatchBatch{
		putEvent(&watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{
			Id:         "a2",
			Name:       "new",
			WorkflowId: "w1",
			Alive:      proto.Bool(true),
		}}}),
	})

	require.Len(t, got.listing.agents, 2)
	selected, ok = got.agents.selectedAgent(got.shared())
	require.True(t, ok)
	assert.Equal(t, "new", selected.Agent.Name, "the event that carries the row leaves the cursor on it")
	assert.True(t, selected.Alive, "and takes the row over")
}

// TestWriteLandsOnTheWorkflowItMade covers a workflow create, whose header is drawn from the
// workflow the write answered with.
func TestWriteLandsOnTheWorkflowItMade(t *testing.T) {
	m := watchedPanel()
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "zeta"}),
	)})

	landed, _ := m.applyWrite(writeResult{written: &written{
		row:      workflowRowKey("w2"),
		workflow: &workflowv1.Workflow{Id: "w2", Name: "api"},
	}})
	got := asModel(t, landed)

	require.Len(t, got.listing.workflows, 2)
	assert.Equal(t, "api", got.listing.workflows[0].Name, "the workflow is filed where the listing would file it")
	assert.Equal(t, workflowRowKey("w2"), rowKeyAt(got.agents.rows(got.shared()), got.agents.cursor))
}

// TestWriteLeavesARowTheWatchAlreadyDelivered covers the write's answer arriving after the
// watch has already put the row on screen, which is left as the watch rendered it.
func TestWriteLeavesARowTheWatchAlreadyDelivered(t *testing.T) {
	m := watchedPanel()
	m.applyWatch(client.WatchBatch{snapshotEvent(
		workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "api"}),
		agentRowOf(&agentv1.Agent{
			Id:         "a2",
			Name:       "new",
			WorkflowId: "w1",
			Alive:      proto.Bool(true),
		}),
	)})

	landed, _ := m.applyWrite(writeResult{written: &written{
		row: agentRowKey("a2"),
		agent: &agentv1.Agent{
			Id:         "a2",
			Name:       "new",
			WorkflowId: "w1",
		},
	}})
	got := asModel(t, landed)

	require.Len(t, got.listing.agents, 1)
	assert.True(t, got.listing.agents[0].Alive)
}

// TestFirstListingLandsOnTheFirstAgent covers the cursor being placed by the first pass.
func TestFirstListingLandsOnTheFirstAgent(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []panel.Workflow{{ID: "w1", Name: "api"}}},
	}

	applyListing(&m, []panel.AgentRow{{Agent: panel.Agent{Name: "api-refactor", WorkflowID: "w1"}}}, []panel.Workflow{{ID: "w1", Name: "api"}}, nil)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok, "the cursor is on an agent rather than on the folder above it")
	assert.Equal(t, "api-refactor", selected.Agent.Name)
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

	applyListing(&m, nil, []panel.Workflow{
		{ID: "w1", Name: "api"},
		{ID: "new", Name: "web"},
	}, nil)

	hovered, ok := m.agents.hoveredWorkflow(m.shared())
	require.True(t, ok, "the cursor is on a folder header")
	assert.Equal(t, "new", hovered.ID)
}

// TestCursorHoldsItsFolderAcrossAListing covers a folder archived from somewhere else, after
// which the cursor follows the folder it names rather than the index it was drawn at.
func TestCursorHoldsItsFolderAcrossAListing(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{workflows: []panel.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}}, agents: []panel.AgentRow{
			{Agent: panel.Agent{
				ID:         "a1",
				Name:       "api-refactor",
				WorkflowID: "w1",
			}},
			{Agent: panel.Agent{
				ID:         "a2",
				Name:       "infra-audit",
				WorkflowID: "w2",
			}},
		}}, agents: agentsScreen{rowCursor: rowCursor{cursor: 2}, placed: true},
	}

	applyListing(&m, []panel.AgentRow{{Agent: panel.Agent{
		ID:         "a2",
		Name:       "infra-audit",
		WorkflowID: "w2",
	}}}, []panel.Workflow{{ID: "w2", Name: "infra"}}, nil)

	hovered, ok := m.agents.hoveredWorkflow(m.shared())
	require.True(t, ok, "the cursor is still on a folder rather than on the agent under it")
	assert.Equal(t, "infra", hovered.Name)
}

// TestCursorHoldsItsAgentWhenAFolderArrives covers a folder sorting in above the cursor,
// where the folders land before the agents and leave the tree half of each pass.
func TestCursorHoldsItsAgentWhenAFolderArrives(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{workflows: []panel.Workflow{{ID: "w2", Name: "infra"}}, agents: []panel.AgentRow{
			{Agent: panel.Agent{
				ID:         "b1",
				Name:       "infra-audit",
				WorkflowID: "w2",
			}},
			{Agent: panel.Agent{
				ID:         "b2",
				Name:       "infra-upgrade",
				WorkflowID: "w2",
			}},
		}}, agents: agentsScreen{rowCursor: rowCursor{cursor: 2}, placed: true},
	}

	applyListing(&m, []panel.AgentRow{
		{Agent: panel.Agent{
			ID:         "b1",
			Name:       "infra-audit",
			WorkflowID: "w2",
		}},
		{Agent: panel.Agent{
			ID:         "b2",
			Name:       "infra-upgrade",
			WorkflowID: "w2",
		}},
	}, []panel.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}}, nil)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok, "the cursor is on an agent")
	assert.Equal(t, "infra-upgrade", selected.Agent.Name)
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

	applyListing(&m, nil, []panel.Workflow{{ID: "w2", Name: "infra"}}, nil)
	m.applyListed(listedMsg{
		archivedListed: true,
		archived: archivedItems([]panel.Agent{
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
	})

	selected, ok := m.archive.hoveredAgent(m.shared())
	require.True(t, ok, "the cursor is on an archived agent")
	assert.Equal(t, "infra-audit", selected.Name)
}

// TestSearchListsTheArchive covers the search screen naming archived agents, which it can only
// do while the screen is listing them.
func TestSearchListsTheArchive(t *testing.T) {
	lists := archivedLists{}

	for _, tc := range []struct {
		screen screen
		want   int
	}{
		{screen: screenSearch, want: 1},
		{screen: screenAgents, want: 0},
	} {
		m := model{
			frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
			keys:   config.DefaultKeybindings(),
			screen: tc.screen,
			api: api{
				agents:    lists,
				workflows: lists,
				schedules: lists,
			},
		}

		m = settle(t, m, m.listCmd())

		assert.Len(t, m.listing.archivedAgents, tc.want, "screen %d holds the archived agents it asked for", tc.screen)
	}
}

// TestTheArchiveIsReadOnlyWhenItMayHaveMoved pins that the archive is read once for the screen
// that draws it, and again only after a watch event that could change it.
func TestTheArchiveIsReadOnlyWhenItMayHaveMoved(t *testing.T) {
	agentPut := func(id string) *watchv1.WatchEvent {
		return putEvent(&watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{Id: id, WorkflowId: "w1"}}})
	}

	tests := []struct {
		name      string
		event     *watchv1.WatchEvent
		wantReads int
	}{
		{
			name:      "a live agent is written",
			event:     agentPut("live"),
			wantReads: 1,
		},
		{
			name:      "a key the panel never saw is gone",
			event:     deletedEvent(watchv1.Resource_RESOURCE_AGENT, "stranger"),
			wantReads: 1,
		},
		{
			name:      "a pull request is gone",
			event:     deletedEvent(watchv1.Resource_RESOURCE_PULL_REQUEST, "a1"),
			wantReads: 1,
		},
		{
			name:      "a live agent is archived",
			event:     deletedEvent(watchv1.Resource_RESOURCE_AGENT, "live"),
			wantReads: 2,
		},
		{
			name:      "an archived agent is deleted",
			event:     deletedEvent(watchv1.Resource_RESOURCE_AGENT, "a1"),
			wantReads: 2,
		},
		{
			name:      "an agent is restored or spawned",
			event:     agentPut("fresh"),
			wantReads: 2,
		},
		{
			name:      "a workflow is restored or made",
			event:     putEvent(&watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: &workflowv1.Workflow{Id: "w2"}}}),
			wantReads: 2,
		},
		{
			name:      "the stream opens again",
			event:     snapshotEvent(),
			wantReads: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lists := &countedArchive{}
			m := model{
				frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
				keys:   config.DefaultKeybindings(),
				screen: screenArchive,
				api: api{
					agents:    lists,
					workflows: lists,
					schedules: lists,
				},
			}
			m.listing.world.Put(&watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{Id: "live", WorkflowId: "w1"}}})

			m = settle(t, m, m.listCmd())
			require.Equal(t, 1, lists.reads, "the screen reads the archive once")
			require.Len(t, m.listing.archivedAgents, 1)

			m = settle(t, m, m.listCmd())
			require.Equal(t, 1, lists.reads, "and not again on the next tick")

			m.applyWatch(client.WatchBatch{tt.event})
			m = settle(t, m, m.listCmd())

			assert.Equal(t, tt.wantReads, lists.reads)
		})
	}
}

// TestReturningToTheArchiveReadsItAgain pins that coming back onto a screen that draws the
// archive reads it fresh.
func TestReturningToTheArchiveReadsItAgain(t *testing.T) {
	lists := &countedArchive{}
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenArchive,
		api: api{
			agents:    lists,
			workflows: lists,
			schedules: lists,
		},
	}

	m = settle(t, m, m.listCmd())
	require.Equal(t, 1, lists.reads)

	m.enterScreen(screenAgents)
	m.enterScreen(screenArchive)
	m = settle(t, m, m.listCmd())

	assert.Equal(t, 2, lists.reads)
}

// countedArchive answers one archived agent and counts how often the archive is read.
type countedArchive struct {
	archivedLists
	reads int
}

func (c *countedArchive) ListAgents(ctx context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	if req.Msg.GetArchived() {
		c.reads++
	}

	return c.archivedLists.ListAgents(ctx, req)
}

type archivedLists struct {
	emptyLists
}

func (archivedLists) ListAgents(_ context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	if !req.Msg.GetArchived() {
		return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
	}

	return connect.NewResponse(&agentv1.ListAgentsResponse{
		Agents: []*agentv1.AgentRow{{Agent: &agentv1.Agent{
			Id:         "a1",
			Name:       "payouts",
			WorkflowId: "w1",
		}}},
	}), nil
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
