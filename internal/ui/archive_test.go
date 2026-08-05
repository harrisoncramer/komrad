package ui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestArchiveKeys covers what a keypress in the archive does, which screen it lands
// on and which write it asks for. Destroying an agent is the one thing the panel
// cannot undo, so the key must open the prompt rather than act, and every key
// that needs a row must do nothing at all on an empty list.
//
// The keys read the row under the cursor, so each one is pressed on a header as well as
// on an agent. Restore means the folder on one and the agent on the other, and delete is
// an agent key that a header has to swallow rather than act on.
func TestArchiveKeys(t *testing.T) {
	keys := config.DefaultKeybindings()

	tests := []struct {
		name string
		// empty starts the screen with nothing archived, for the keys that act on a row.
		empty bool
		// archivedFolder puts the workflow itself in the archive, so the header is one
		// that can be restored rather than a live folder holding a put-away agent.
		archivedFolder bool
		// cursor is 0 on the workflow header and 1 on the agent nested under it.
		cursor               int
		key                  string
		wantScreen           screen
		wantRestored         []string
		wantRestoredWorkflow []string
		wantDeleted          []string
	}{
		{
			name:       "back returns to the agents tree",
			key:        "esc",
			cursor:     1,
			wantScreen: screenAgents,
		},
		{
			name:         "restore puts the hovered agent back",
			key:          "r",
			cursor:       1,
			wantScreen:   screenArchive,
			wantRestored: []string{"a1"},
		},
		{
			// Nothing is destroyed on the keypress itself. The prompt is the whole
			// safety here, since the same key on the agents list is reversible.
			name:       "delete opens the confirm prompt rather than destroying",
			key:        "d",
			cursor:     1,
			wantScreen: screenArchiveDelete,
		},
		{
			name:                 "restore on an archived folder restores the workflow",
			archivedFolder:       true,
			key:                  "r",
			wantScreen:           screenArchive,
			wantRestoredWorkflow: []string{"w1"},
		},
		{
			// The folder is still running, so there is nothing about it to bring back.
			// Its agents are restored one at a time from the rows beneath it.
			name:       "restore on a live folder's header asks for no write",
			key:        "r",
			wantScreen: screenArchive,
		},
		{
			name:           "delete on a folder opens no prompt, since nothing hard-deletes one",
			archivedFolder: true,
			key:            "d",
			wantScreen:     screenArchive,
		},
		{
			name:       "restore on an empty archive asks for no write",
			empty:      true,
			key:        "r",
			wantScreen: screenArchive,
		},
		{
			name:       "delete on an empty archive opens no prompt",
			empty:      true,
			key:        "d",
			wantScreen: screenArchive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeArchiveAgents{}
			workflows := &fakeArchiveWorkflows{}
			m := archiveModel(agents, workflows, keys)
			m.archive.cursor = tt.cursor
			if tt.archivedFolder {
				m.tree.workflows = nil
				m.archive.workflows = []workflowstore.Workflow{archivedWorkflow("w1", "backend")}
			}
			if tt.empty {
				m.tree.workflows, m.archive.agents = nil, nil
			}

			updated, cmd := m.updateArchive(keyMsg(tt.key))
			pressed, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantRestored, agents.restored)
			assert.Equal(t, tt.wantRestoredWorkflow, workflows.restored)
			assert.Equal(t, tt.wantDeleted, agents.deleted)
		})
	}
}

// TestArchiveRows covers the shape the archive draws. A folder put away whole keeps a
// header even with nothing under it, since that row is the only way to restore it, and a
// folder still running gets one too, as the container for the agents put away out of it.
func TestArchiveRows(t *testing.T) {
	tests := []struct {
		name      string
		live      []workflowstore.Workflow
		archived  []workflowstore.Workflow
		agents    []agentstore.Agent
		wantRows  []string
		wantEmpty bool
	}{
		{
			name:      "nothing archived draws nothing",
			live:      []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			wantRows:  []string{},
			wantEmpty: true,
		},
		{
			name:     "an archived folder emptied of its agents keeps its header",
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend")},
			wantRows: []string{"header:backend"},
		},
		{
			name:     "an agent archived out of a live folder nests under it",
			live:     []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents:   []agentstore.Agent{{ID: "a1", Name: "auth", WorkflowID: "w1"}},
			wantRows: []string{"header:backend", "agent:auth"},
		},
		{
			name:     "a live folder with nothing put away out of it gets no header",
			live:     []workflowstore.Workflow{{ID: "w1", Name: "backend"}, {ID: "w2", Name: "frontend"}},
			agents:   []agentstore.Agent{{ID: "a1", Name: "auth", WorkflowID: "w1"}},
			wantRows: []string{"header:backend", "agent:auth"},
		},
		{
			name:     "the two kinds of folder interleave by id, the order the agents screen uses",
			live:     []workflowstore.Workflow{{ID: "w2", Name: "frontend"}},
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend"), archivedWorkflow("w3", "infra")},
			agents: []agentstore.Agent{
				{ID: "a1", Name: "auth", WorkflowID: "w1"},
				{ID: "a2", Name: "ui", WorkflowID: "w2"},
			},
			wantRows: []string{"header:backend", "agent:auth", "header:frontend", "agent:ui", "header:infra"},
		},
		{
			// The two listings run at once, so a restore landing between them can put the
			// same folder in both. Drawn twice it would nest its agents twice.
			name:     "a folder in both listings is drawn once",
			live:     []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend")},
			agents:   []agentstore.Agent{{ID: "a1", Name: "auth", WorkflowID: "w1"}},
			wantRows: []string{"header:backend", "agent:auth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				keys:    config.DefaultKeybindings(),
				tree:    treeState{workflows: tt.live},
				archive: archiveState{agents: tt.agents, workflows: tt.archived},
			}

			assert.Equal(t, tt.wantRows, describeRows(m.archiveRows()))

			if tt.wantEmpty {
				assert.Contains(t, m.archiveView(), "nothing archived")
			}
		})
	}
}

// TestArchiveHelpSplitsOnTheRow covers the legend saying what the row under the cursor
// takes. Restore means a different thing on a folder than on an agent, delete is not
// offered on a folder at all, and a live folder's header takes no key of its own.
func TestArchiveHelpSplitsOnTheRow(t *testing.T) {
	tests := []struct {
		name        string
		archived    bool
		cursor      int
		wants       []string
		wantsAbsent []string
	}{
		{
			name:        "on an archived folder",
			archived:    true,
			wants:       []string{"restore workflow"},
			wantsAbsent: []string{"delete for good"},
		},
		{
			name:        "on a live folder",
			cursor:      0,
			wantsAbsent: []string{"restore", "delete for good"},
		},
		{
			name:   "on an agent",
			cursor: 1,
			wants:  []string{"restore", "delete for good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
			m.archive.cursor = tt.cursor
			m.helpExpanded = true
			if tt.archived {
				m.tree.workflows = nil
				m.archive.workflows = []workflowstore.Workflow{archivedWorkflow("w1", "backend")}
			}

			got := ansi.Strip(m.renderHelp(m.archiveHelp()))

			for _, want := range tt.wants {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantsAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestArchiveDeleteConfirm covers the prompt itself. Declining has to leave the
// agent alone, since the prompt exists precisely because the write cannot be
// taken back.
func TestArchiveDeleteConfirm(t *testing.T) {
	tests := []struct {
		name        string
		confirm     bool
		wantDeleted []string
	}{
		{
			name:        "confirming destroys the agent it was opened on",
			confirm:     true,
			wantDeleted: []string{"a1"},
		},
		{
			name:    "declining destroys nothing",
			confirm: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeArchiveAgents{}
			m := archiveModel(client, &fakeArchiveWorkflows{}, config.DefaultKeybindings())

			opened, _ := m.deleteFromArchiveConfirm(m.archive.agents)
			m, ok := opened.(model)
			require.True(t, ok)
			require.Equal(t, screenArchiveDelete, m.screen)

			answer := "n"
			if tt.confirm {
				answer = "y"
			}

			got := answerPrompt(t, m, answer)

			assert.Equal(t, tt.wantDeleted, client.deleted)
			assert.Equal(t, screenArchive, got.screen, "the prompt returns to the archive it was opened from")
			assert.Nil(t, got.target.deleteAgents)
		})
	}
}

// answerPrompt presses one key on the delete prompt and runs the commands the
// form hands back, which is what bubbletea does for it on screen. The form only
// reaches its completed state once those have run, so a test that dropped them
// would watch a prompt that never closes. Once the prompt is off screen the answer
// has been acted on, and what it asked for is settled the way the runtime would.
func answerPrompt(t *testing.T, m model, key string) model {
	t.Helper()

	var msg tea.Msg = keyMsg(key)
	for range 8 {
		updated, cmd := m.updateArchiveDelete(msg)
		next, ok := updated.(model)
		require.True(t, ok)

		m = next
		if cmd == nil {
			return m
		}
		if m.screen != screenArchiveDelete {
			return settle(t, m, cmd)
		}

		msg = cmd()
	}

	t.Fatal("the delete prompt never closed")

	return m
}

// TestRefusedArchiveWritesAreReported covers the archive's writes failing. All of them
// leave the row exactly where it was, so without the banner the keypress reads as one
// that never registered.
func TestRefusedArchiveWritesAreReported(t *testing.T) {
	tests := []struct {
		name string
		// onFolder puts the cursor on an archived folder's header rather than on an agent.
		onFolder    bool
		act         func(t *testing.T, m model) model
		wantSummary string
	}{
		{
			name:        "a refused restore",
			act:         pressRestore,
			wantSummary: `Could not restore agent "api-refactor"`,
		},
		{
			name:        "a refused workflow restore",
			onFolder:    true,
			act:         pressRestore,
			wantSummary: `Could not restore workflow "backend"`,
		},
		{
			name: "a refused delete",
			act: func(t *testing.T, m model) model {
				t.Helper()
				opened, _ := m.deleteFromArchiveConfirm(m.archive.agents)
				got, ok := opened.(model)
				require.True(t, ok)

				return answerPrompt(t, got, "y")
			},
			wantSummary: `Could not delete agent "api-refactor"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refusal := connect.NewError(connect.CodeNotFound, errors.New("no such agent"))
			m := archiveModel(
				&fakeArchiveAgents{err: refusal},
				&fakeArchiveWorkflows{err: refusal},
				config.DefaultKeybindings(),
			)
			if tt.onFolder {
				m.tree.workflows = nil
				m.archive.workflows = []workflowstore.Workflow{archivedWorkflow("w1", "backend")}
				m.archive.cursor = 0
			}

			got := tt.act(t, m)

			require.NotNil(t, got.banner)
			assert.Equal(t, tt.wantSummary, got.banner.Summary)
			assert.Equal(t, "no such agent", got.banner.Detail)
		})
	}
}

// pressRestore presses restore on whatever the cursor is on and settles what it asked for.
func pressRestore(t *testing.T, m model) model {
	t.Helper()

	updated, cmd := m.restoreFromArchive()
	pressed, ok := updated.(model)
	require.True(t, ok)

	return settle(t, pressed, cmd)
}

// TestArchiveView covers what the list renders. An empty archive has to say so rather
// than drawing a blank pane, which reads as a screen that failed to load, and a folder
// put away whole has to read differently from one that is still running, since restore
// acts on only one of the two.
func TestArchiveView(t *testing.T) {
	tests := []struct {
		name        string
		live        []workflowstore.Workflow
		archived    []workflowstore.Workflow
		agents      []agentstore.Agent
		wants       []string
		wantsAbsent []string
	}{
		{
			name:  "an empty archive says so",
			wants: []string{"Archive", "nothing archived"},
		},
		{
			name:   "an archived agent shows its name under its folder",
			live:   []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentstore.Agent{{ID: "a1", Name: "api-refactor", WorkflowID: "w1"}},
			wants:  []string{"backend", "api-refactor"},
			// The folder is still running, so nothing on its row says otherwise.
			wantsAbsent: []string{"archived"},
		},
		{
			name:     "an archived folder says it is put away",
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend")},
			wants:    []string{"backend", "archived"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
			m.tree.workflows = tt.live
			m.archive = archiveState{agents: tt.agents, workflows: tt.archived}

			view := ansi.Strip(m.archiveView())
			for _, want := range tt.wants {
				assert.Contains(t, view, want)
			}
			for _, absent := range tt.wantsAbsent {
				assert.NotContains(t, view, absent)
			}
		})
	}
}

// TestArchiveMarkdown covers the record in the stage. An archived agent has no pane,
// so the record must not read as one that is merely dormant and waiting to be
// opened.
func TestArchiveMarkdown(t *testing.T) {
	archivedAt := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	got := archiveMarkdown(agentstore.Agent{
		Name: "api-refactor", Cwd: "/work/api", Flags: "--model opus", SessionID: "sess-1",
		ArchivedAt: &archivedAt,
	}, "nightly").markdown

	assert.Contains(t, got, "api-refactor")
	assert.Contains(t, got, "archived")
	assert.Contains(t, got, "/work/api")
	assert.Contains(t, got, "nightly")
	assert.Contains(t, got, "archived: ", "the record says when it was put away")
	assert.NotContains(t, got, "dormant", "an archived agent is not one waiting to be opened")
}

// TestArchiveIsOrderedByWhenAgentsWereArchived covers the order the archive reads
// in. The listing arrives keyed on the id, which orders by when each agent was
// created, so an agent archived long after it was made would otherwise sit below one
// created after it.
func TestArchiveIsOrderedByWhenAgentsWereArchived(t *testing.T) {
	tests := []struct {
		name      string
		listed    []*agentv1.Agent
		wantNames []string
	}{
		{
			name: "the most recently archived agent is first",
			listed: []*agentv1.Agent{
				archivedProtoAgent("archived-at-nine", "2026-07-30T09:00:00Z"),
				archivedProtoAgent("archived-at-noon", "2026-07-30T12:00:00Z"),
			},
			wantNames: []string{"archived-at-noon", "archived-at-nine"},
		},
		{
			name: "a row archived before the time was recorded sits below the ones that carry it",
			listed: []*agentv1.Agent{
				{Name: "unstamped"},
				archivedProtoAgent("stamped", "2026-07-30T09:00:00Z"),
			},
			wantNames: []string{"stamped", "unstamped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := archivedAgentsFrom(tt.listed)

			names := make([]string, 0, len(got))
			for _, agent := range got {
				names = append(names, agent.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// archivedProtoAgent builds a listed agent archived at the given RFC 3339 time.
func archivedProtoAgent(name, archivedAt string) *agentv1.Agent {
	return &agentv1.Agent{Name: name, ArchivedAt: &archivedAt}
}

// archivedProtoWorkflow builds a listed workflow archived at the given RFC 3339 time.
func archivedProtoWorkflow(id, name, archivedAt string) *workflowv1.Workflow {
	return &workflowv1.Workflow{Id: id, Name: name, ArchivedAt: &archivedAt}
}

// TestArchiveIsListedSeparatelyFromTheAgents covers the four listings staying apart. A
// pass reads the archived agents and the archived folders through calls of their own, so
// neither can ever appear as a row in the tree.
func TestArchiveIsListedSeparatelyFromTheAgents(t *testing.T) {
	agents := &fakeArchiveAgents{}
	workflows := &fakeArchiveWorkflows{
		archived: []*workflowv1.Workflow{archivedProtoWorkflow("w9", "retired", "2026-07-30T09:00:00Z")},
	}
	m := archiveModel(agents, workflows, config.DefaultKeybindings())
	m.archive.agents = nil

	entered, cmd := m.enterArchive()
	opened, ok := entered.(model)
	require.True(t, ok)

	got := settle(t, opened, cmd)

	assert.Contains(t, agents.listedArchived, true, "the archive is asked for by the pass the screen starts")
	assert.Contains(t, workflows.listed, true, "and so are the folders it draws over them")
	require.Len(t, got.archive.agents, 1)
	assert.Equal(t, "api-refactor", got.archive.agents[0].Name)
	require.Len(t, got.archive.workflows, 1)
	assert.NotNil(t, got.archive.workflows[0].ArchivedAt, "an archived folder carries when it was put away")
	assert.Empty(t, got.tree.agents, "the tree's rows are not what the archive listed")
	assert.Empty(t, got.tree.workflows, "and the archived folder is not one of the tree's")
}

// archiveModel builds the panel sitting on the archive with one agent archived out of a
// workflow that is still live, the cursor on the agent rather than on the header over it.
func archiveModel(agents agentv1connect.AgentServiceClient, workflows workflowv1connect.WorkflowServiceClient, keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api:    api{agents: agents, workflows: workflows, schedules: emptyLists{}},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		stage:  NewStage(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenArchive,
		tree:   treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}},
		archive: archiveState{
			agents:    []agentstore.Agent{{ID: "a1", Name: "api-refactor", WorkflowID: "w1"}},
			rowCursor: rowCursor{cursor: 1},
		},
	}
}

// archivedWorkflow is a workflow put away whole, which the archive draws as a header of
// its own so it has a row to be restored from.
func archivedWorkflow(id, name string) workflowstore.Workflow {
	at := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)

	return workflowstore.Workflow{ID: id, Name: name, ArchivedAt: &at}
}

// fakeArchiveWorkflows answers the calls the archive makes for folders, recording which
// it was asked to restore. The embedded interface is nil, so any other method panics
// rather than quietly returning a zero value.
type fakeArchiveWorkflows struct {
	workflowv1connect.WorkflowServiceClient
	mu       sync.Mutex
	err      error
	archived []*workflowv1.Workflow
	restored []string
	listed   []bool
}

func (f *fakeArchiveWorkflows) ListWorkflows(_ context.Context, req *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	f.mu.Lock()
	f.listed = append(f.listed, req.Msg.GetArchived())
	f.mu.Unlock()

	if !req.Msg.GetArchived() {
		return connect.NewResponse(&workflowv1.ListWorkflowsResponse{}), nil
	}

	return connect.NewResponse(&workflowv1.ListWorkflowsResponse{Workflows: f.archived}), nil
}

func (f *fakeArchiveWorkflows) RestoreWorkflow(_ context.Context, req *connect.Request[workflowv1.RestoreWorkflowRequest]) (*connect.Response[workflowv1.RestoreWorkflowResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.restored = append(f.restored, req.Msg.GetId())

	return connect.NewResponse(&workflowv1.RestoreWorkflowResponse{}), nil
}

// fakeArchiveAgents answers the calls the archive makes, recording which agents it
// was asked to restore and destroy and which set it listed. The embedded
// interface is nil, so any other method panics rather than quietly returning a
// zero value.
//
// A pass runs its listings at once, so what it records is guarded: the live agents
// and the archived ones are two calls on this same fake on two goroutines.
//
// ResumeAgent is left off deliberately. Restoring from the archive puts the row back
// and starts nothing, so a resume reaching here panics rather than passing.
type fakeArchiveAgents struct {
	agentv1connect.AgentServiceClient
	mu             sync.Mutex
	err            error
	restored       []string
	deleted        []string
	listedArchived []bool
}

func (f *fakeArchiveAgents) ListAgents(_ context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	f.mu.Lock()
	f.listedArchived = append(f.listedArchived, req.Msg.GetArchived())
	f.mu.Unlock()

	if !req.Msg.GetArchived() {
		return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
	}

	return connect.NewResponse(&agentv1.ListAgentsResponse{
		Agents: []*agentv1.Agent{{Id: "a1", Name: "api-refactor"}},
	}), nil
}

func (f *fakeArchiveAgents) RestoreAgents(_ context.Context, req *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.restored = append(f.restored, req.Msg.GetIds()...)

	response := &agentv1.RestoreAgentsResponse{}
	for _, id := range req.Msg.GetIds() {
		response.Agents = append(response.Agents, &agentv1.Agent{Id: id, Name: "api-refactor"})
	}

	return connect.NewResponse(response), nil
}

func (f *fakeArchiveAgents) DeleteAgents(_ context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.deleted = append(f.deleted, req.Msg.GetIds()...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}
