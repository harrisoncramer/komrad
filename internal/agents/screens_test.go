package agents

import (
	"testing"

	"komrad/internal/harness/pane"
	"komrad/internal/harnesstest"
	"komrad/internal/store"
	"komrad/internal/tmuxtest"

	agentstore "komrad/internal/store/agent"
	screenstore "komrad/internal/store/screen"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcileKeepsTheScreenAnAgentRestsOn covers which frames the reconciler saves.
func TestReconcileKeepsTheScreenAnAgentRestsOn(t *testing.T) {
	type pass struct {
		frame  string
		screen string
	}

	tests := []struct {
		name   string
		passes []pass
		want   string
	}{
		{
			name:   "an idle pane is saved",
			passes: []pass{{frame: harnesstest.FrameIdle, screen: "❯ idle"}},
			want:   "❯ idle",
		},
		{
			name:   "a question waiting on an answer is saved",
			passes: []pass{{frame: harnesstest.FrameWaiting, screen: "❯ waiting"}},
			want:   "❯ waiting",
		},
		{
			name:   "a working pane is not saved",
			passes: []pass{{frame: harnesstest.FrameWorking, screen: "✻ working"}},
		},
		{
			name: "a frame already saved is not taken again",
			passes: []pass{
				{frame: harnesstest.FrameIdle, screen: "❯ first"},
				{frame: harnesstest.FrameIdle, screen: "❯ second"},
			},
			want: "❯ first",
		},
		{
			name: "a frame that changed is taken again",
			passes: []pass{
				{frame: harnesstest.FrameIdle, screen: "❯ first"},
				{frame: harnesstest.FrameComposing, screen: "❯ second"},
			},
			want: "❯ second",
		},
		{
			name: "work between two rests leaves the rest before it",
			passes: []pass{
				{frame: harnesstest.FrameIdle, screen: "❯ first"},
				{frame: harnesstest.FrameWorking, screen: "✻ working"},
			},
			want: "❯ first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = agent.ID
			tmux.Sizes["%1"] = tmuxtest.Size{Width: 120, Height: 40}

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			}).WithScreenKeeper(NewScreenKeeper(NewScreenKeeperParams{Panes: tmux, Screens: st.Screens}))

			for _, pass := range tt.passes {
				tmux.Captures["%1"] = pass.frame
				tmux.Screens["%1"] = pass.screen
				require.NoError(t, rec.Reconcile(t.Context()))
			}

			saved, err := st.Screens.Get(t.Context(), agent.ID)
			if tt.want == "" {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, saved.Frame)
			assert.Equal(t, 120, saved.Width)
			assert.Equal(t, 40, saved.Height)
		})
	}
}

// TestArchiveSavesTheScreen covers the screen taken as an agent is archived.
func TestArchiveSavesTheScreen(t *testing.T) {
	tests := []struct {
		name    string
		command string
		owned   bool
		want    string
	}{
		{
			name:  "a pane running its CLI is saved before it is killed",
			owned: true,
			want:  "❯ last words",
		},
		{
			name:    "a pane fallen back to a shell keeps the screen saved before",
			command: "zsh",
			owned:   true,
			want:    "❯ earlier",
		},
		{
			name: "a pane another agent owns keeps the screen saved before",
			want: "❯ earlier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")

			agent := seed(t, st, "target", "%1", agentstore.StateIdle)
			if tt.owned {
				require.NoError(t, tmux.SetPaneOwner("%1", agent.ID))
			}
			tmux.Commands["%1"] = tt.command
			tmux.Screens["%1"] = "❯ last words"
			tmux.Sizes["%1"] = tmuxtest.Size{Width: 100, Height: 30}
			require.NoError(t, st.Screens.Upsert(t.Context(), screenstore.UpsertParams{AgentID: agent.ID, Frame: "❯ earlier", Width: 80, Height: 24}))

			require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), []string{agent.ID}))

			saved, err := st.Screens.Get(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, saved.Frame)
		})
	}
}

// TestArchiveWorkflowSavesItsAgentsScreens covers the screens taken as a workflow is archived.
func TestArchiveWorkflowSavesItsAgentsScreens(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()

	agent := seedRunning(t, st, tmux, "inside", "%1", agentstore.StateIdle)
	tmux.Screens["%1"] = "❯ inside"
	tmux.Sizes["%1"] = tmuxtest.Size{Width: 80, Height: 24}

	require.NoError(t, newWorkflowService(st, tmux).ArchiveWorkflow(t.Context(), agent.WorkflowID))

	saved, err := st.Screens.Get(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "❯ inside", saved.Frame)
}
