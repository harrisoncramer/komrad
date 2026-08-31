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
		name string

		empty bool

		archivedFolder bool

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
			assert.False(t, got.form.active(), "the prompt is down")
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
			assert.False(t, got.form.active(), "the prompt is down")
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
			assert.False(t, got.form.active(), "the prompt is down")
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
		name string

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

			require.NotNil(t, got.banner)
			assert.Equal(t, tt.wantSummary, got.banner.Summary)
			assert.Equal(t, "no such agent", got.banner.Detail)
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
			wants: []string{"backend", "api-refactor"},

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

// TestArchiveMarkdown covers the document the stage pages for an archived agent, which must
// not read as one merely dormant and waiting to be opened.
func TestArchiveMarkdown(t *testing.T) {
	got := archiveMarkdown(agentstore.Agent{Name: "api-refactor"})

	assert.Contains(t, got, "api-refactor")
	assert.Contains(t, got, "archived")
	assert.Contains(t, got, "Restore brings it back")
	assert.NotContains(t, got, "dormant", "an archived agent is not one waiting to be opened")
}

// TestArchiveStagesWhyADeleteLeftARowBehind covers the stage for an agent whose cleanup
// action failed, since the archive is the only place that failure is legible.
func TestArchiveStagesWhyADeleteLeftARowBehind(t *testing.T) {
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.listing.workflows = []workflowstore.Workflow{{ID: "w1", Name: "backend"}}
	m.archive = archiveState{agents: []agentstore.Agent{{
		ID:         "a1",
		Name:       "api-refactor",
		WorkflowID: "w1",
		State:      agentstore.StateFailedToCleanup,
		Metadata:   []agentstore.Metadata{{Key: failureReasonKey, Value: "the worktree is dirty"}},
	}}}

	staged := m.archiveDocument(m.archive.agents[0])

	assert.Contains(t, staged, "teardown failed")
	assert.Contains(t, staged, "the worktree is dirty")
	assert.NotContains(t, staged, "Restore brings it back", "this row is not one merely waiting to be restored")
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
