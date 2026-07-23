package agents

import (
	"context"
	"testing"

	"claude-agents/internal/store"
	"claude-agents/internal/tmux"
)

// manager builds an AgentManager over a real store and a tmux client pointed at
// a socket that does not exist. Hook is given an explicit session name, so it
// never needs to ask tmux for the current session, keeping the test hermetic.
func manager(t *testing.T) (*AgentManager, *store.Store) {
	t.Helper()
	st := newStore(t)
	m := NewAgentManager(Params{Tmux: tmux.NewClient("claude-agents-nosock"), Store: st})
	return m, st
}

// TestHookIgnoresUntrackedSessions is the guard against external Claude sessions
// leaking in: they fire the same global hooks but the tool never created a row
// for them, so the hook must be a no-op.
func TestHookIgnoresUntrackedSessions(t *testing.T) {
	m, st := manager(t)
	ctx := context.Background()

	m.Hook("prompt", "external", "%1", "sid")
	if all, _ := st.ListAll(ctx); len(all) != 0 {
		t.Fatalf("hook for an untracked session created %d rows", len(all))
	}
}

// TestHookAdvancesTrackedAgent walks a tool-created agent through the lifecycle,
// including the guards that keep a finished agent finished.
func TestHookAdvancesTrackedAgent(t *testing.T) {
	m, st := manager(t)
	ctx := context.Background()

	if err := st.Upsert(ctx, store.Agent{Name: "mine", State: store.StateNew}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	steps := []struct {
		mode string
		want string
	}{
		{"prompt", store.StateThinking},
		{"notification", store.StateWaiting},
		{"posttooluse", store.StateThinking},
		{"notification", store.StateWaiting},
		{"stop", store.StateDone},
		{"notification", store.StateDone}, // a notification after done stays done
		{"posttooluse", store.StateDone},  // post-tool only revives a waiting agent
	}
	for i, step := range steps {
		m.Hook(step.mode, "mine", "%2", "sid")
		got, _ := st.Get(ctx, "mine")
		if got == nil || got.State != step.want {
			t.Fatalf("step %d (%s): want %q, got %+v", i, step.mode, step.want, got)
		}
	}
}
