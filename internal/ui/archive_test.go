package ui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"weave/internal/appconfig"
	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestArchiveKeys covers what a keypress in the archive does, which screen it lands
// on and which write it asks for. Destroying an agent is the one thing the panel
// cannot undo, so the key must open the prompt rather than act, and every key
// that needs a row must do nothing at all on an empty list.
func TestArchiveKeys(t *testing.T) {
	keys := appconfig.DefaultKeybindings()

	tests := []struct {
		name string
		// empty starts the screen with nothing archived, for the keys that act on a row.
		empty        bool
		key          string
		wantScreen   screen
		wantRestored []string
		wantDeleted  []string
	}{
		{
			name:       "back returns to the agents tree",
			key:        "esc",
			wantScreen: screenAgents,
		},
		{
			name:         "restore puts the hovered agent back",
			key:          "r",
			wantScreen:   screenArchive,
			wantRestored: []string{"a1"},
		},
		{
			// Nothing is destroyed on the keypress itself. The prompt is the whole
			// safety here, since the same key on the agents list is reversible.
			name:       "delete opens the confirm prompt rather than destroying",
			key:        "d",
			wantScreen: screenArchiveDelete,
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
			client := &fakeArchiveAgents{}
			m := archiveModel(client, keys)
			if tt.empty {
				m.archive.agents = nil
			}

			updated, cmd := m.updateArchive(keyMsg(tt.key))
			pressed, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantRestored, client.restored)
			assert.Equal(t, tt.wantDeleted, client.deleted)
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
			m := archiveModel(client, appconfig.DefaultKeybindings())

			opened, _ := m.deleteFromArchiveConfirm()
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
			assert.Nil(t, got.target.deleteAgent)
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

// TestRefusedArchiveWritesAreReported covers the archive's two writes failing. Both
// leave the row exactly where it was, so without the banner the keypress reads
// as one that never registered.
func TestRefusedArchiveWritesAreReported(t *testing.T) {
	tests := []struct {
		name        string
		act         func(t *testing.T, m model) model
		wantSummary string
	}{
		{
			name: "a refused restore",
			act: func(t *testing.T, m model) model {
				t.Helper()
				updated, cmd := m.restoreFromArchive()
				pressed, ok := updated.(model)
				require.True(t, ok)

				return settle(t, pressed, cmd)
			},
			wantSummary: `Could not restore agent "api-refactor"`,
		},
		{
			name: "a refused delete",
			act: func(t *testing.T, m model) model {
				t.Helper()
				opened, _ := m.deleteFromArchiveConfirm()
				got, ok := opened.(model)
				require.True(t, ok)

				return answerPrompt(t, got, "y")
			},
			wantSummary: `Could not delete agent "api-refactor"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeArchiveAgents{err: connect.NewError(connect.CodeNotFound, errors.New("no such agent"))}
			m := archiveModel(client, appconfig.DefaultKeybindings())

			got := tt.act(t, m)

			require.NotNil(t, got.banner)
			assert.Equal(t, tt.wantSummary, got.banner.Summary)
			assert.Equal(t, "no such agent", got.banner.Detail)
		})
	}
}

// TestArchiveView covers what the list renders. An empty archive has to say so
// rather than drawing a blank pane, which reads as a screen that failed to load.
func TestArchiveView(t *testing.T) {
	tests := []struct {
		name    string
		archive []agentstore.Agent
		wants   []string
	}{
		{
			name:  "an empty archive says so",
			wants: []string{"Archive", "nothing archived"},
		},
		{
			name: "an archived agent shows its name and what can be done to it",
			archive: []agentstore.Agent{
				{ID: "a1", Name: "api-refactor"},
			},
			wants: []string{"api-refactor", "restore", "delete for good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := archiveModel(&fakeArchiveAgents{}, appconfig.DefaultKeybindings())
			m.archive.agents = tt.archive
			m.archive.cursor = 0

			view := m.archiveView()
			for _, want := range tt.wants {
				assert.Contains(t, view, want)
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
	}, "nightly")

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

// TestArchiveIsListedSeparatelyFromTheAgents covers the two listings staying
// apart. A pass reads the archived agents through a call of their own, so an
// archived agent can never appear as a row in the tree.
func TestArchiveIsListedSeparatelyFromTheAgents(t *testing.T) {
	client := &fakeArchiveAgents{}
	m := archiveModel(client, appconfig.DefaultKeybindings())
	m.archive.agents = nil

	entered, cmd := m.enterArchive()
	opened, ok := entered.(model)
	require.True(t, ok)

	got := settle(t, opened, cmd)

	assert.Contains(t, client.listedArchived, true, "the archive is asked for by the pass the screen starts")
	require.Len(t, got.archive.agents, 1)
	assert.Equal(t, "api-refactor", got.archive.agents[0].Name)
	assert.Empty(t, got.tree.agents, "the tree's rows are not what the archive listed")
}

// archiveModel builds the panel sitting on the archive with one archived agent in it.
func archiveModel(client agentv1connect.AgentServiceClient, keys appconfig.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api:     api{agents: client, workflows: emptyLists{}, schedules: emptyLists{}},
		tmux:    tmux,
		popup:   NewPopup(tmux, "test-session"),
		stage:   NewStage(tmux, appconfig.DefaultSplit),
		keys:    keys,
		screen:  screenArchive,
		archive: archiveState{agents: []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}},
	}
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

func (f *fakeArchiveAgents) RestoreAgent(_ context.Context, req *connect.Request[agentv1.RestoreAgentRequest]) (*connect.Response[agentv1.RestoreAgentResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.restored = append(f.restored, req.Msg.GetId())

	return connect.NewResponse(&agentv1.RestoreAgentResponse{
		Agent: &agentv1.Agent{Id: req.Msg.GetId(), Name: "api-refactor"},
	}), nil
}

func (f *fakeArchiveAgents) DeleteAgent(_ context.Context, req *connect.Request[agentv1.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.deleted = append(f.deleted, req.Msg.GetId())

	return connect.NewResponse(&emptypb.Empty{}), nil
}
