package attaches_test

import (
	"testing"

	"komrad/internal/attaches"
	"komrad/internal/pty"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/storetest"
	"komrad/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpen covers which agents a terminal attaches to and what it attaches to.
func TestOpen(t *testing.T) {
	stores := storetest.Open(t)
	live := stores.SeedAgent(t, agentstore.Agent{Name: "live", Pane: "%9"})
	paneless := stores.SeedAgent(t, agentstore.Agent{Name: "paneless"})
	stolen := stores.SeedAgent(t, agentstore.Agent{Name: "stolen", Pane: "%8"})
	archived := stores.SeedAgent(t, agentstore.Agent{Name: "archived", Pane: "%7"})
	_, err := stores.Agents.Archive(t.Context(), []string{archived.ID})
	require.NoError(t, err)
	deleting := stores.SeedAgent(t, agentstore.Agent{Name: "deleting", Pane: "%6"})
	require.NoError(t, stores.Agents.MarkDeleting(t.Context(), []string{deleting.ID}))

	fake := tmuxtest.NewFake()
	fake.Owners["%9"] = live.ID
	fake.Owners["%8"] = "someone-else"
	fake.Owners["%7"] = archived.ID
	fake.Owners["%6"] = deleting.ID

	service := attaches.NewAttachService(attaches.NewAttachServiceParams{Agents: stores.Agents, Tmux: fake, Session: "komrad"})

	tests := []struct {
		name     string
		agentID  string
		wantErr  error
		wantView string
	}{
		{
			name:    "an agent that does not exist is refused",
			agentID: "01a0debb-d80a-71a5-9f85-000000000000",
			wantErr: store.ErrNotFound,
		},
		{
			name:    "an archived agent is refused",
			agentID: archived.ID,
			wantErr: attaches.ErrNotLive,
		},
		{
			name:    "an agent being deleted is refused",
			agentID: deleting.ID,
			wantErr: attaches.ErrNotLive,
		},
		{
			name:    "an agent with no pane is refused",
			agentID: paneless.ID,
			wantErr: attaches.ErrNoPane,
		},
		{
			name:    "an agent whose pane another agent holds is refused",
			agentID: stolen.ID,
			wantErr: attaches.ErrNoPane,
		},
		{
			name:     "a live agent holding its pane is attached through a view of that pane",
			agentID:  live.ID,
			wantView: "view-9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal, err := service.Open(t.Context(), attaches.OpenParams{AgentID: tt.agentID, Size: pty.Size{Rows: 24, Cols: 80}})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			t.Cleanup(func() { _ = terminal.Close() })

			code, err := terminal.Wait()
			require.NoError(t, err)
			assert.Zero(t, code)
			assert.Equal(t, "%9", fake.Views[tt.wantView])
		})
	}
}

// TestRelease covers when a view is killed once an attach to it ends.
func TestRelease(t *testing.T) {
	stores := storetest.Open(t)
	agent := stores.SeedAgent(t, agentstore.Agent{Name: "live", Pane: "%9"})

	open := func(t *testing.T, service *attaches.AttachService) pty.Terminal {
		t.Helper()

		terminal, err := service.Open(t.Context(), attaches.OpenParams{AgentID: agent.ID, Size: pty.Size{Rows: 24, Cols: 80}})
		require.NoError(t, err)
		t.Cleanup(func() { _ = terminal.Close() })

		return terminal
	}

	newService := func(fake *tmuxtest.Fake) *attaches.AttachService {
		fake.Owners["%9"] = agent.ID

		return attaches.NewAttachService(attaches.NewAttachServiceParams{Agents: stores.Agents, Tmux: fake, Session: "komrad"})
	}

	t.Run("the last attach to end kills the view", func(t *testing.T) {
		fake := tmuxtest.NewFake()
		service := newService(fake)

		first := open(t, service)
		second := open(t, service)

		_, err := first.Wait()
		require.NoError(t, err)
		assert.Empty(t, fake.ReadKilledSessions(), "another attach still holds the view")

		_, err = second.Wait()
		require.NoError(t, err)
		assert.Equal(t, []string{"view-9"}, fake.ReadKilledSessions())
	})

	t.Run("a view a client outside komrad is attached to is kept", func(t *testing.T) {
		fake := tmuxtest.NewFake()
		fake.SessionClients["view-9"] = 1
		service := newService(fake)

		_, err := open(t, service).Wait()
		require.NoError(t, err)
		assert.Empty(t, fake.ReadKilledSessions())
	})

	t.Run("an attach that cannot start lets its view go", func(t *testing.T) {
		fake := tmuxtest.NewFake()
		fake.AttachCommand = []string{"/nonexistent/tmux"}
		service := newService(fake)

		_, err := service.Open(t.Context(), attaches.OpenParams{AgentID: agent.ID, Size: pty.Size{Rows: 24, Cols: 80}})
		require.Error(t, err)
		assert.Equal(t, []string{"view-9"}, fake.ReadKilledSessions())
	})
}
