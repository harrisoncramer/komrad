package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
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

// TestArchiveKeys covers what each key does on a header and on an agent, where delete opens
// the prompt rather than acting because the panel cannot undo it.
func TestArchiveKeys(t *testing.T) {
	keys := config.DefaultKeybindings()

	tests := []struct {
		name                 string
		empty                bool
		archivedFolder       bool
		cursor               int
		key                  string
		wantScreen           screen
		wantForm             formKind
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
			name:       "delete opens the confirm prompt rather than destroying",
			key:        "d",
			cursor:     1,
			wantScreen: screenArchive,
			wantForm:   formArchiveDelete,
		},
		{
			name:                 "restore on an archived folder restores the workflow",
			archivedFolder:       true,
			key:                  "r",
			wantScreen:           screenArchive,
			wantRestoredWorkflow: []string{"w1"},
		},
		{
			name:       "restore on a live folder's header asks for no write",
			key:        "r",
			wantScreen: screenArchive,
		},
		{
			name:           "delete on an archived folder opens the confirm prompt",
			archivedFolder: true,
			key:            "d",
			wantScreen:     screenArchive,
			wantForm:       formArchiveDelete,
		},
		{
			name:       "delete on a live folder's header asks for no write",
			key:        "d",
			wantScreen: screenArchive,
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
		{
			name:       "empty opens the confirm prompt rather than sweeping",
			key:        "D",
			cursor:     1,
			wantScreen: screenArchive,
			wantForm:   formArchiveEmpty,
		},
		{
			name:       "empty on a header opens it too, since it is the whole archive it acts on",
			key:        "D",
			wantScreen: screenArchive,
			wantForm:   formArchiveEmpty,
		},
		{
			name:       "empty on an empty archive opens no prompt",
			empty:      true,
			key:        "D",
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
				m.listing.workflows = nil
				m.archive.workflows = []workflowstore.Workflow{archivedWorkflow("w1", "backend")}
			}
			if tt.empty {
				m.listing.workflows, m.archive.agents = nil, nil
			}

			updated, cmd := m.updateArchive(keyMsg(tt.key))
			pressed, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantForm, got.form.kind, "the prompt that opened, if any")
			assert.Equal(t, tt.wantRestored, agents.restored)
			assert.Equal(t, tt.wantRestoredWorkflow, workflows.restored)
			assert.Equal(t, tt.wantDeleted, agents.deleted)
		})
	}
}

// TestArchiveRows covers the shape the archive draws, where a folder keeps a header even
// with nothing under it since that row is the only way to restore it.
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
			name: "an agent archived out of a live folder nests under it",
			live: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentstore.Agent{{
				ID:         "a1",
				Name:       "auth",
				WorkflowID: "w1",
			}},
			wantRows: []string{"header:backend", "agent:auth"},
		},
		{
			name: "a live folder with nothing put away out of it gets no header",
			live: []workflowstore.Workflow{{ID: "w1", Name: "backend"}, {ID: "w2", Name: "frontend"}},
			agents: []agentstore.Agent{{
				ID:         "a1",
				Name:       "auth",
				WorkflowID: "w1",
			}},
			wantRows: []string{"header:backend", "agent:auth"},
		},
		{
			name:     "the two kinds of folder interleave by id, the order the agents screen uses",
			live:     []workflowstore.Workflow{{ID: "w2", Name: "frontend"}},
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend"), archivedWorkflow("w3", "infra")},
			agents: []agentstore.Agent{
				{
					ID:         "a1",
					Name:       "auth",
					WorkflowID: "w1",
				},
				{
					ID:         "a2",
					Name:       "ui",
					WorkflowID: "w2",
				},
			},
			wantRows: []string{"header:backend", "agent:auth", "header:frontend", "agent:ui", "header:infra"},
		},
		{
			name:     "a folder in both listings is drawn once",
			live:     []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend")},
			agents: []agentstore.Agent{{
				ID:         "a1",
				Name:       "auth",
				WorkflowID: "w1",
			}},
			wantRows: []string{"header:backend", "agent:auth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				keys:    config.DefaultKeybindings(),
				listing: listing{workflows: tt.live},
				archive: archiveState{agents: tt.agents, workflows: tt.archived},
			}

			assert.Equal(t, tt.wantRows, describeRows(m.archiveRows()))

			if tt.wantEmpty {
				assert.Contains(t, m.archiveView(), "nothing archived")
			}
		})
	}
}

// TestArchiveHelpSplitsOnTheRow covers the legend saying what the row under the cursor takes,
// since restore means a different thing on a folder than on an agent.
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
				m.listing.workflows = nil
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

// TestArchiveDeleteConfirm covers the prompt, where declining leaves the agent alone since
// the write cannot be taken back.
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
			require.Equal(t, formArchiveDelete, m.form.kind)

			answer := "n"
			if tt.confirm {
				answer = "y"
			}

			got := answerPrompt(t, m, answer)

			assert.Equal(t, tt.wantDeleted, client.deleted)
			assert.Equal(t, screenArchive, got.screen, "the prompt returns to the archive it was opened from")
			assert.False(t, got.form.isActive(), "the prompt is down")
		})
	}
}

// TestArchiveDeleteWorkflowConfirm covers the confirm behind deleting a workflow out of the
// archive, the same shape as deleting a single agent: a decline destroys nothing, a confirm
// asks for the workflow by id and clears the target either way.
func TestArchiveDeleteWorkflowConfirm(t *testing.T) {
	tests := []struct {
		name        string
		confirm     bool
		wantDeleted []string
	}{
		{
			name:        "confirming destroys the workflow it was opened on",
			confirm:     true,
			wantDeleted: []string{"w1"},
		},
		{
			name:    "declining destroys nothing",
			confirm: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflows := &fakeArchiveWorkflows{}
			m := archiveModel(&fakeArchiveAgents{}, workflows, config.DefaultKeybindings())

			opened, _ := m.deleteWorkflowConfirm(archivedWorkflow("w1", "backend"))
			m, ok := opened.(model)
			require.True(t, ok)
			require.Equal(t, formArchiveDelete, m.form.kind)

			answer := "n"
			if tt.confirm {
				answer = "y"
			}

			got := answerPrompt(t, m, answer)

			assert.Equal(t, tt.wantDeleted, workflows.deleted)
			assert.Equal(t, screenArchive, got.screen, "the prompt returns to the archive it was opened from")
			assert.False(t, got.form.isActive(), "the prompt is down")
		})
	}
}

// TestArchiveEmptyConfirm covers the sweep behind the confirm, which destroys every archived
// agent at once and so must queue nothing when it is declined.
func TestArchiveEmptyConfirm(t *testing.T) {
	tests := []struct {
		name        string
		confirm     bool
		wantEmptied int
	}{
		{
			name:        "confirming queues the sweep",
			confirm:     true,
			wantEmptied: 1,
		},
		{
			name:    "declining queues nothing",
			confirm: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeArchiveAgents{}
			m := archiveModel(client, &fakeArchiveWorkflows{}, config.DefaultKeybindings())

			opened, _ := m.emptyArchiveConfirm()
			m, ok := opened.(model)
			require.True(t, ok)
			require.Equal(t, formArchiveEmpty, m.form.kind)

			answer := "n"
			if tt.confirm {
				answer = "y"
			}

			got := answerEmptyPrompt(t, m, answer)

			assert.Equal(t, tt.wantEmptied, client.emptied)
			assert.Equal(t, screenArchive, got.screen, "the prompt returns to the archive it was opened from")
			assert.False(t, got.form.isActive(), "the prompt is down")
		})
	}
}

// TestEmptyArchivePromptStartsOnCancel covers the prompt opening on the answer that costs
// nothing, since nothing in the panel undoes emptying the archive.
func TestEmptyArchivePromptStartsOnCancel(t *testing.T) {
	client := &fakeArchiveAgents{}
	keys := config.DefaultKeybindings()
	m := archiveModel(client, &fakeArchiveWorkflows{}, keys)

	opened, _ := m.emptyArchiveConfirm()
	m, ok := opened.(model)
	require.True(t, ok)

	got := answerEmptyPrompt(t, m, keys.Form.Submit[0])

	assert.Zero(t, client.emptied, "a submit that answered nothing queues no sweep")
	assert.Equal(t, screenArchive, got.screen)
}

func answerEmptyPrompt(t *testing.T, m model, key string) model {
	t.Helper()

	var msg tea.Msg = keyMsg(key)
	for range 8 {
		updated, cmd := m.form.update(m, msg)
		next, ok := updated.(model)
		require.True(t, ok)

		m = next
		if cmd == nil {
			return m
		}
		if m.form.kind != formArchiveEmpty {
			return settle(t, m, cmd)
		}

		msg = cmd()
	}

	t.Fatal("the empty prompt never closed")

	return m
}

func answerPrompt(t *testing.T, m model, key string) model {
	t.Helper()

	var msg tea.Msg = keyMsg(key)
	for range 8 {
		updated, cmd := m.form.update(m, msg)
		next, ok := updated.(model)
		require.True(t, ok)

		m = next
		if cmd == nil {
			return m
		}
		if m.form.kind != formArchiveDelete {
			return settle(t, m, cmd)
		}

		msg = cmd()
	}

	t.Fatal("the delete prompt never closed")

	return m
}

// TestRefusedArchiveWritesAreReported covers a failed write, which leaves the row where it
// was and so needs the banner to look like anything happened at all.
func TestRefusedArchiveWritesAreReported(t *testing.T) {
	tests := []struct {
		name        string
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
				m.listing.workflows = nil
				m.archive.workflows = []workflowstore.Workflow{archivedWorkflow("w1", "backend")}
				m.archive.cursor = 0
			}

			got := tt.act(t, m)

			require.NotEmpty(t, got.messages.items)
			assert.Equal(t, tt.wantSummary, newestMessage(got).summary)
			assert.Equal(t, "no such agent", newestMessage(got).detail)
		})
	}
}

func pressRestore(t *testing.T, m model) model {
	t.Helper()

	updated, cmd := m.restoreFromArchive()
	pressed, ok := updated.(model)
	require.True(t, ok)

	return settle(t, pressed, cmd)
}

// TestArchiveView covers what the list renders, where an empty archive says so rather than
// drawing a blank pane that reads as a screen which failed to load.
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
			name: "an archived agent shows its name under its folder",
			live: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentstore.Agent{{
				ID:         "a1",
				Name:       "api-refactor",
				WorkflowID: "w1",
			}},
			wants:       []string{"backend", "api-refactor"},
			wantsAbsent: []string{"archived"},
		},
		{
			name:     "an archived folder says it is put away",
			archived: []workflowstore.Workflow{archivedWorkflow("w1", "backend")},
			wants:    []string{"backend", "archived"},
		},
		{
			name: "a row a delete could not destroy is marked",
			live: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentstore.Agent{{
				ID:         "a1",
				Name:       "api-refactor",
				WorkflowID: "w1",
				State:      agentstore.StateFailedToCleanup,
			}},
			wants: []string{"api-refactor", "✗"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
			m.listing.workflows = tt.live
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

// TestArchiveMarkdown covers the document drawn beside the list for an archived agent, which
// must not read as one merely dormant and waiting to be opened.
func TestArchiveMarkdown(t *testing.T) {
	got := archiveMarkdown(agentstore.Agent{Name: "api-refactor"})

	assert.Contains(t, got, "api-refactor")
	assert.Contains(t, got, "archived")
	assert.Contains(t, got, "Restore brings it back")
	assert.NotContains(t, got, "dormant", "an archived agent is not one waiting to be opened")
}

// TestArchiveDrawsWhyADeleteLeftARowBehind covers the document for an agent whose deprovisioner
// failed, since the archive is the only place that failure is legible.
func TestArchiveDrawsWhyADeleteLeftARowBehind(t *testing.T) {
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.listing.workflows = []workflowstore.Workflow{{ID: "w1", Name: "backend"}}
	m.archive = archiveState{agents: []agentstore.Agent{{
		ID:         "a1",
		Name:       "api-refactor",
		WorkflowID: "w1",
		State:      agentstore.StateFailedToCleanup,
		Metadata:   []agentstore.Metadata{{Key: failureReasonKey, Value: "the worktree is dirty"}},
	}}}

	drawn := m.archiveDocument(m.archive.agents[0])

	assert.Contains(t, drawn, "teardown failed")
	assert.Contains(t, drawn, "the worktree is dirty")
	assert.NotContains(t, drawn, "Restore brings it back", "this row is not one merely waiting to be restored")
}

// TestArchiveFields covers what the details under the list draw for an archived agent: what
// the agents list shows for a live one, plus when it was put away.
func TestArchiveFields(t *testing.T) {
	archivedAt := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	fields := archiveFields(agentstore.Agent{
		Name: "api-refactor", Cwd: "/work/api", Flags: "--model opus", SessionID: "sess-1",
		ArchivedAt: &archivedAt,
	}, "nightly")

	assert.Contains(t, fields, recordField{"directory", "/work/api"})
	assert.Contains(t, fields, recordField{"flags", "--model opus"})
	assert.Contains(t, fields, recordField{"session_id", "sess-1"})
	assert.Contains(t, fields, recordField{"launched by", "nightly"})

	labels := make([]string, 0, len(fields))
	for _, field := range fields {
		labels = append(labels, field.label)
	}
	assert.Contains(t, labels, "archived", "the record says when it was put away")
}

// TestArchiveIsOrderedByWhenAgentsWereArchived covers the archive's order, since the listing
// arrives keyed on the id and would otherwise sort by when each agent was created.
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

func archivedProtoAgent(name, archivedAt string) *agentv1.Agent {
	return &agentv1.Agent{Name: name, ArchivedAt: &archivedAt}
}

func archivedProtoWorkflow(id, name, archivedAt string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:         id,
		Name:       name,
		ArchivedAt: &archivedAt,
	}
}

// TestArchiveIsListedSeparatelyFromTheAgents covers the archived agents and folders arriving
// through calls of their own, so neither can appear as a row in the tree.
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
	assert.Empty(t, got.listing.agents, "the tree's rows are not what the archive listed")
	assert.Empty(t, got.listing.workflows, "and the archived folder is not one of the tree's")
}

func archiveModel(agents agentv1connect.AgentServiceClient, workflows workflowv1connect.WorkflowServiceClient, keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api: api{
			agents:    agents,
			workflows: workflows,
			schedules: emptyLists{},
		},
		tmux:    tmux,
		popup:   NewPopupWindow(tmux, "test-session"),
		frame:   layout.New(tmux, config.DefaultLayout()),
		keys:    keys,
		screen:  screenArchive,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}},
		archive: archiveState{
			agents: []agentstore.Agent{{
				ID:         "a1",
				Name:       "api-refactor",
				WorkflowID: "w1",
			}},
			rowCursor: rowCursor{cursor: 1},
		},
	}
}

func archivedWorkflow(id, name string) workflowstore.Workflow {
	at := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)

	return workflowstore.Workflow{
		ID:         id,
		Name:       name,
		ArchivedAt: &at,
	}
}

type fakeArchiveWorkflows struct {
	workflowv1connect.WorkflowServiceClient
	mu       sync.Mutex
	err      error
	archived []*workflowv1.Workflow
	restored []string
	deleted  []string
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

func (f *fakeArchiveWorkflows) DeleteWorkflow(_ context.Context, req *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.deleted = append(f.deleted, req.Msg.GetId())

	return connect.NewResponse(&emptypb.Empty{}), nil
}

type fakeArchiveAgents struct {
	agentv1connect.AgentServiceClient
	mu             sync.Mutex
	err            error
	restored       []string
	deleted        []string
	emptied        int
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

func (f *fakeArchiveAgents) EmptyArchive(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.emptied++

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *fakeArchiveAgents) DeleteAgents(_ context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.deleted = append(f.deleted, req.Msg.GetIds()...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// splitArchiveModel is the archive holding the whole window, which is the state it draws its own
// two columns in.
func splitArchiveModel(t *testing.T) model {
	t.Helper()

	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.width, m.height = 140, 24
	m.frame.Attach("%0")
	m.frame.Solo(true)

	return m
}

// TestArchiveDrawsTheDocumentBesideTheList pins the collapse itself: the row the cursor is on is
// drawn in the panel's own frame rather than pushed into a pager in a pane of its own.
func TestArchiveDrawsTheDocumentBesideTheList(t *testing.T) {
	m := splitArchiveModel(t)
	m.listing.workflows = []workflowstore.Workflow{{
		ID:      "w1",
		Name:    "backend",
		Context: "the shared notes",
	}}

	view := ansi.Strip(m.View())

	assert.Contains(t, view, "api-refactor", "the list is still drawn")
	assert.Contains(t, view, "kept until deleted", "and the hovered row's document beside it")

	for _, row := range lines(view) {
		assert.LessOrEqual(t, ansi.StringWidth(row), m.width, "neither column runs past the window")
	}

	m.landArchiveCursor(m.archiveRows(), 0)
	assert.Contains(t, ansi.Strip(m.View()), "the shared notes", "the document follows the cursor onto the folder")
}

// TestArchiveDrawsNothingArchivedBesideTheList covers the empty archive, which has no row to
// describe and so used to be the one document nothing hovered.
func TestArchiveDrawsNothingArchivedBesideTheList(t *testing.T) {
	m := splitArchiveModel(t)
	m.archive = archiveState{}
	m.listing.workflows = nil

	view := ansi.Strip(m.View())

	assert.Contains(t, view, "nothing archived", "the list says the archive is empty")
	assert.Contains(t, view, "until you delete them", "and the notice beside it says what the archive is for")
}

// TestArchiveScrollsTheDocument covers the scrolling less was doing before the document was drawn
// in the panel's own frame: it moves, it says what is past each end, and it stops at both.
func TestArchiveScrollsTheDocument(t *testing.T) {
	m := splitArchiveModel(t)
	m.landArchiveCursor(m.archiveRows(), 0)
	m.listing.workflows = []workflowstore.Workflow{{
		ID:      "w1",
		Name:    "backend",
		Context: strings.Repeat("a sentence long enough to wrap the column it is drawn into. ", 60),
	}}

	split, ok := m.screenColumnSplit()
	require.True(t, ok, "the archive holds the window")
	require.Greater(t, len(docRows(m.archiveDoc(), split.column)), split.rows, "the document runs past the window")

	top := ansi.Strip(m.View())
	assert.NotContains(t, top, "↑", "nothing is above the first screenful")
	assert.Contains(t, top, "↓", "and the reader is told the document goes on")

	m = m.scrollColumn(1)
	assert.Equal(t, docScrollStep, m.column.top)
	assert.Contains(t, ansi.Strip(m.View()), "↑", "the reader is told what they scrolled past")

	for range 100 {
		m = m.scrollColumn(1)
	}

	bottom := ansi.Strip(m.View())
	assert.NotContains(t, bottom, "↓", "the last row of the document is reachable")
	assert.Contains(t, bottom, "↑")

	for range 100 {
		m = m.scrollColumn(-1)
	}

	assert.Zero(t, m.column.top, "and scrolling back stops at the top rather than going negative")
}

// TestArchiveCursorTakesTheDocumentBackToItsTop covers the offset belonging to the row it was
// scrolled on, since the next row is a document the reader has not seen the start of.
func TestArchiveCursorTakesTheDocumentBackToItsTop(t *testing.T) {
	m := splitArchiveModel(t)
	m.column.top = 12

	m = m.moveArchiveCursor(-1)
	assert.Zero(t, m.column.top, "the cursor landed on another row")

	m.column.top = 12
	m = m.moveArchiveCursor(-1)
	assert.Equal(t, 12, m.column.top, "a cursor that could not move leaves the offset where it was")
}

// TestArchiveHandsTheWindowBack covers the zoom, which has to go for the screen the reader
// leaves for, since what stands beside the list there is somebody else's own pane.
func TestArchiveHandsTheWindowBack(t *testing.T) {
	tmux := &recordingTmux{}
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.tmux, m.popup = tmux, NewPopupWindow(tmux, "test-session")
	m.frame = layout.New(tmux, config.DefaultLayout())
	m.frame.Attach("%0")

	entered, _ := m.enterArchive()
	m, ok := entered.(model)
	require.True(t, ok)
	assert.True(t, m.frame.IsSolo(), "the archive draws both columns itself, so it takes the window")

	// A live agent's own pane is what stands beside the list there, so the panel gives the window
	// back on the way to a row that has one.
	m.listing.agents = []agentItem{{agent: agentstore.Agent{
		ID:         "a1",
		Name:       "auth",
		Pane:       "%9",
		WorkflowID: "w1",
	}, alive: true}}
	m.tree.cursor = 1

	left, _ := m.updateArchive(keyMsg("esc"))
	m, ok = left.(model)
	require.True(t, ok)
	assert.False(t, m.frame.IsSolo(), "leaving the archive for a running agent gives the other panes their share back")
}

// TestArchiveHandsTheWindowBackWhenAWriteLands covers the agent create started on the list that
// only lands once the reader has opened the archive, whose new pane would otherwise be shown under
// a zoom the cursor has not caught up to yet.
func TestArchiveHandsTheWindowBackWhenAWriteLands(t *testing.T) {
	m := splitArchiveModel(t)
	require.True(t, m.frame.IsSolo(), "the archive holds the window to begin with")

	landed, _ := m.applyWrite(writeResult{written: &written{row: agentRowKey("a1"), pane: "%9"}})
	got, ok := landed.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, got.screen, "the write puts the reader on the row it wrote")
	assert.False(t, got.frame.IsSolo(), "and the pane it made is not left under the archive's own zoom")
}

// TestArchiveKeepsTheOffsetWhenARowSlides covers a refresh landing while the reader is scrolled
// down a document: an agent archived above the one they are on moves its index without moving
// them, and the offset belongs to the row rather than to the place it sits in the list.
func TestArchiveKeepsTheOffsetWhenARowSlides(t *testing.T) {
	m := splitArchiveModel(t)
	held := []agentstore.Agent{{
		ID:         "a2",
		Name:       "middle",
		WorkflowID: "w1",
	}}
	m.applyArchive(held, nil, "")

	rows := m.archiveRows()
	m.landArchiveCursor(rows, rowIndexOfKey(rows, agentRowKey("a2")))
	m.column.top = 20

	was := m.archive.cursor
	grown := append([]agentstore.Agent{{
		ID:         "a0",
		Name:       "alpha",
		WorkflowID: "w1",
	}}, held...)
	m.applyArchive(grown, nil, agentRowKey("a2"))

	assert.Greater(t, m.archive.cursor, was, "the row the reader is on has slid down the list")
	assert.Equal(t, 20, m.column.top, "but the reader has not moved, so neither has the document")

	m.applyArchive(grown, nil, agentRowKey("a0"))
	assert.Zero(t, m.column.top, "landing on another row does start that one at its top")
}

// TestArchiveGivesUpTheSplitWhenTheListWillNotFit covers the legend, which gives up its columns as
// the sidebar narrows and stands taller for it: a short window that held the list whole cannot
// hold it once the document has taken half, so the document waits rather than the list running off
// the bottom of the pane.
func TestArchiveGivesUpTheSplitWhenTheListWillNotFit(t *testing.T) {
	m := splitArchiveModel(t)
	m.width, m.height = 120, 10
	m.helpExpanded = true

	_, ok := m.screenColumnSplit()
	assert.False(t, ok, "ten rows cannot hold the list beside the document")
	assert.Equal(t, m.width, m.paneWidth(), "so the list is drawn in the whole of the pane")
	assert.LessOrEqual(t, countLines(m.View()), m.height, "and stays inside it")

	m.height = 24
	split, ok := m.screenColumnSplit()
	require.True(t, ok, "a taller window holds both halves")
	assert.Equal(t, split.sidebar, m.paneWidth(), "and the list is laid out against its own column")
	assert.LessOrEqual(t, countLines(m.View()), m.height)
}

// TestArchiveDocViewFillsItsColumn covers the document's own column at the row counts the join is
// least forgiving of: the block stands exactly as tall as its half of the window at every offset,
// since a block that falls short lets the list beside it slide underneath.
func TestArchiveDocViewFillsItsColumn(t *testing.T) {
	m := splitArchiveModel(t)
	m.landArchiveCursor(m.archiveRows(), 0)
	m.listing.workflows = []workflowstore.Workflow{{
		ID:      "w1",
		Name:    "backend",
		Context: strings.Repeat("a line of the document. ", 30),
	}}

	total := len(docRows(m.archiveDoc(), 60))
	require.Greater(t, total, 5, "the document runs past every window this walks")

	for rows := 1; rows <= 5; rows++ {
		for _, top := range []int{0, 1, 2, total - 1, total, total + 5} {
			m.column.top = top
			drawn := m.docColumnView(columnSplit{
				column:  60,
				sidebar: 30,
				rows:    rows,
			})

			assert.Equal(t, rows, countLines(drawn), "rows=%d top=%d", rows, top)
		}
	}
}
