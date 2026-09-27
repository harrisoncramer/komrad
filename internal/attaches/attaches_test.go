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
	_, err = stores.Agents.MarkDeleting(t.Context(), []string{deleting.ID})
	require.NoError(t, err)

	fake := tmuxtest.NewFake()
	fake.Owners["%9"] = live.ID
	fake.Owners["%8"] = "someone-else"
	fake.Owners["%7"] = archived.ID
	fake.Owners["%6"] = deleting.ID

	service := attaches.NewAttachService(attaches.NewAttachServiceParams{
		Agents:  stores.Agents,
		Tmux:    fake,
		Session: "komrad",
	})

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

	tests := []struct {
		name           string
		attachCommand  []string
		sessionClients int
		attaches       int
		waited         int
		wantOpenErr    bool
		wantKilled     []string
	}{
		{
			name:          "an attach that cannot start lets its view go",
			attachCommand: []string{"/nonexistent/tmux"},
			attaches:      1,
			wantOpenErr:   true,
			wantKilled:    []string{"view-9"},
		},
		{
			name:           "a view a client outside komrad is attached to is kept",
			sessionClients: 1,
			attaches:       1,
			waited:         1,
		},
		{
			name:     "a view another attach still holds is kept",
			attaches: 2,
			waited:   1,
		},
		{
			name:       "the last attach to end kills the view",
			attaches:   2,
			waited:     2,
			wantKilled: []string{"view-9"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := tmuxtest.NewFake()
			fake.Owners["%9"] = agent.ID
			fake.SessionClients["view-9"] = tt.sessionClients
			if tt.attachCommand != nil {
				fake.AttachCommand = tt.attachCommand
			}

			service := attaches.NewAttachService(attaches.NewAttachServiceParams{
				Agents:  stores.Agents,
				Tmux:    fake,
				Session: "komrad",
			})

			terminals := make([]pty.Terminal, 0, tt.attaches)
			for range tt.attaches {
				terminal, err := service.Open(t.Context(), attaches.OpenParams{AgentID: agent.ID, Size: pty.Size{Rows: 24, Cols: 80}})
				if tt.wantOpenErr {
					require.Error(t, err)

					continue
				}

				require.NoError(t, err)
				t.Cleanup(func() { _ = terminal.Close() })
				terminals = append(terminals, terminal)
			}

			for _, terminal := range terminals[:tt.waited] {
				_, err := terminal.Wait()
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantKilled, fake.ReadKilledSessions())
		})
	}
}
