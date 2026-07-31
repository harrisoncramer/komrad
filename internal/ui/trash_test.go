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

// TestTrashKeys covers what a keypress in the trash does, which screen it lands
// on and which write it asks for. Destroying an agent is the one thing the panel
// cannot undo, so the key must open the prompt rather than act, and every key
// that needs a row must do nothing at all on an empty list.
func TestTrashKeys(t *testing.T) {
	keys := appconfig.DefaultKeybindings()

	tests := []struct {
		name string
		// empty starts the screen with nothing deleted, for the keys that act on a row.
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
			wantScreen:   screenTrash,
			wantRestored: []string{"a1"},
		},
		{
			// Nothing is destroyed on the keypress itself. The prompt is the whole
			// safety here, since the same key on the agents list is reversible.
			name:       "delete opens the confirm prompt rather than destroying",
			key:        "d",
			wantScreen: screenTrashDelete,
		},
		{
			name:       "restore on an empty trash asks for no write",
			empty:      true,
			key:        "r",
			wantScreen: screenTrash,
		},
		{
			name:       "delete on an empty trash opens no prompt",
			empty:      true,
			key:        "d",
			wantScreen: screenTrash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeTrashAgents{}
			m := trashModel(client, keys)
			if tt.empty {
				m.trash.agents = nil
			}

			updated, cmd := m.updateTrash(keyMsg(tt.key))
			pressed, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantRestored, client.restored)
			assert.Equal(t, tt.wantDeleted, client.deleted)
		})
	}
}

// TestTrashDeleteConfirm covers the prompt itself. Declining has to leave the
// agent alone, since the prompt exists precisely because the write cannot be
// taken back.
func TestTrashDeleteConfirm(t *testing.T) {
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
			client := &fakeTrashAgents{}
			m := trashModel(client, appconfig.DefaultKeybindings())

			opened, _ := m.deleteFromTrashConfirm()
			m, ok := opened.(model)
			require.True(t, ok)
			require.Equal(t, screenTrashDelete, m.screen)

			answer := "n"
			if tt.confirm {
				answer = "y"
			}

			got := answerPrompt(t, m, answer)

			assert.Equal(t, tt.wantDeleted, client.deleted)
			assert.Equal(t, screenTrash, got.screen, "the prompt returns to the trash it was opened from")
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
		updated, cmd := m.updateTrashDelete(msg)
		next, ok := updated.(model)
		require.True(t, ok)

		m = next
		if cmd == nil {
			return m
		}
		if m.screen != screenTrashDelete {
			return settle(t, m, cmd)
		}

		msg = cmd()
	}

	t.Fatal("the delete prompt never closed")

	return m
}

// TestRefusedTrashWritesAreReported covers the trash's two writes failing. Both
// leave the row exactly where it was, so without the banner the keypress reads
// as one that never registered.
func TestRefusedTrashWritesAreReported(t *testing.T) {
	tests := []struct {
		name        string
		act         func(t *testing.T, m model) model
		wantSummary string
	}{
		{
			name: "a refused restore",
			act: func(t *testing.T, m model) model {
				t.Helper()
				updated, cmd := m.restoreFromTrash()
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
				opened, _ := m.deleteFromTrashConfirm()
				got, ok := opened.(model)
				require.True(t, ok)

				return answerPrompt(t, got, "y")
			},
			wantSummary: `Could not delete agent "api-refactor"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeTrashAgents{err: connect.NewError(connect.CodeNotFound, errors.New("no such agent"))}
			m := trashModel(client, appconfig.DefaultKeybindings())

			got := tt.act(t, m)

			require.NotNil(t, got.actionErr)
			assert.Equal(t, tt.wantSummary, got.actionErr.Summary)
			assert.Equal(t, "no such agent", got.actionErr.Detail)
		})
	}
}

// TestTrashView covers what the list renders. An empty trash has to say so
// rather than drawing a blank pane, which reads as a screen that failed to load.
func TestTrashView(t *testing.T) {
	tests := []struct {
		name  string
		trash []agentstore.Agent
		wants []string
	}{
		{
			name:  "an empty trash says so",
			wants: []string{"Trash", "nothing deleted"},
		},
		{
			name: "a deleted agent shows its name and what can be done to it",
			trash: []agentstore.Agent{
				{ID: "a1", Name: "api-refactor"},
			},
			wants: []string{"api-refactor", "restore", "delete for good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := trashModel(&fakeTrashAgents{}, appconfig.DefaultKeybindings())
			m.trash.agents = tt.trash
			m.trash.cursor = 0

			view := m.trashView()
			for _, want := range tt.wants {
				assert.Contains(t, view, want)
			}
		})
	}
}

// TestTrashMarkdown covers the record in the stage. A deleted agent has no pane,
// so the record must not read as one that is merely dormant and waiting to be
// opened.
func TestTrashMarkdown(t *testing.T) {
	deletedAt := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	got := trashMarkdown(agentstore.Agent{
		Name: "api-refactor", Cwd: "/work/api", Flags: "--model opus", SessionID: "sess-1",
		DeletedAt: &deletedAt,
	}, "nightly")

	assert.Contains(t, got, "api-refactor")
	assert.Contains(t, got, "deleted")
	assert.Contains(t, got, "/work/api")
	assert.Contains(t, got, "nightly")
	assert.Contains(t, got, "deleted: ", "the record says when it was thrown away")
	assert.NotContains(t, got, "dormant", "a deleted agent is not one waiting to be opened")
}

// TestTrashIsOrderedByWhenAgentsWereDeleted covers the order the trash reads in.
// The listing arrives keyed on the id, which orders by when each agent was
// created, so an agent deleted long after it was made would otherwise sit below one
// created after it.
func TestTrashIsOrderedByWhenAgentsWereDeleted(t *testing.T) {
	tests := []struct {
		name      string
		listed    []*agentv1.Agent
		wantNames []string
	}{
		{
			name: "the most recently deleted agent is first",
			listed: []*agentv1.Agent{
				deletedProtoAgent("newest-agent", "2026-07-30T09:00:00Z"),
				deletedProtoAgent("oldest-agent", "2026-07-30T12:00:00Z"),
			},
			wantNames: []string{"oldest-agent", "newest-agent"},
		},
		{
			name: "a row deleted before the time was recorded sits below the ones that carry it",
			listed: []*agentv1.Agent{
				{Name: "unstamped"},
				deletedProtoAgent("stamped", "2026-07-30T09:00:00Z"),
			},
			wantNames: []string{"stamped", "unstamped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deletedAgentsFrom(tt.listed)

			names := make([]string, 0, len(got))
			for _, agent := range got {
				names = append(names, agent.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// deletedProtoAgent builds a listed agent deleted at the given RFC 3339 time.
func deletedProtoAgent(name, deletedAt string) *agentv1.Agent {
	return &agentv1.Agent{Name: name, DeletedAt: &deletedAt}
}

// TestTrashIsListedSeparatelyFromTheAgents covers the two listings staying
// apart. A pass reads the deleted agents through a call of their own, so a deleted
// agent can never appear as a row in the tree.
func TestTrashIsListedSeparatelyFromTheAgents(t *testing.T) {
	client := &fakeTrashAgents{}
	m := trashModel(client, appconfig.DefaultKeybindings())
	m.trash.agents = nil

	entered, cmd := m.enterTrash()
	opened, ok := entered.(model)
	require.True(t, ok)

	got := settle(t, opened, cmd)

	assert.Contains(t, client.listedDeleted, true, "the trash is asked for by the pass the screen starts")
	require.Len(t, got.trash.agents, 1)
	assert.Equal(t, "api-refactor", got.trash.agents[0].Name)
	assert.Empty(t, got.tree.agents, "the tree's rows are not what the trash listed")
}

// trashModel builds the panel sitting on the trash with one deleted agent in it.
func trashModel(client agentv1connect.AgentServiceClient, keys appconfig.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api:    api{agents: client, workflows: emptyLists{}, schedules: emptyLists{}},
		tmux:   tmux,
		popup:  NewPopup(tmux, "test-session"),
		stage:  NewStage(tmux, appconfig.DefaultSplit),
		keys:   keys,
		screen: screenTrash,
		trash:  trashState{agents: []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}},
	}
}

// fakeTrashAgents answers the calls the trash makes, recording which agents it
// was asked to restore and destroy and which set it listed. The embedded
// interface is nil, so any other method panics rather than quietly returning a
// zero value.
//
// A pass runs its listings at once, so what it records is guarded: the live agents
// and the deleted ones are two calls on this same fake on two goroutines.
type fakeTrashAgents struct {
	agentv1connect.AgentServiceClient
	mu            sync.Mutex
	err           error
	restored      []string
	deleted       []string
	listedDeleted []bool
}

func (f *fakeTrashAgents) ListAgents(_ context.Context, req *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	f.mu.Lock()
	f.listedDeleted = append(f.listedDeleted, req.Msg.GetDeleted())
	f.mu.Unlock()

	if !req.Msg.GetDeleted() {
		return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
	}

	return connect.NewResponse(&agentv1.ListAgentsResponse{
		Agents: []*agentv1.Agent{{Id: "a1", Name: "api-refactor"}},
	}), nil
}

func (f *fakeTrashAgents) RestoreAgent(_ context.Context, req *connect.Request[agentv1.RestoreAgentRequest]) (*connect.Response[agentv1.RestoreAgentResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.restored = append(f.restored, req.Msg.GetId())

	return connect.NewResponse(&agentv1.RestoreAgentResponse{
		Agent: &agentv1.Agent{Id: req.Msg.GetId(), Name: "api-refactor"},
	}), nil
}

func (f *fakeTrashAgents) DeleteAgent(_ context.Context, req *connect.Request[agentv1.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.deleted = append(f.deleted, req.Msg.GetId())

	return connect.NewResponse(&emptypb.Empty{}), nil
}
