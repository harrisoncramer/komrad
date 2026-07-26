package cli

import (
	"path/filepath"
	"testing"

	"claude-agents/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStore opens a throwaway store for the status summary to read.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "agents.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return st
}

// seed records one agent in the given state.
func seed(t *testing.T, st *store.Store, name, state string) {
	t.Helper()
	require.NoError(t, st.Upsert(t.Context(), store.Agent{Name: name, State: state}))
}

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		name         string
		states       []string
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "no agents renders nothing",
			states:    nil,
			wantEmpty: true,
		},
		{
			name:      "only new agents render nothing",
			states:    []string{store.StateNew, store.StateNew},
			wantEmpty: true,
		},
		{
			name:         "done agents alone render a done segment",
			states:       []string{store.StateDone},
			wantContains: []string{"1 done"},
		},
		{
			name:         "a mix renders every non-empty segment",
			states:       []string{store.StateWaiting, store.StateThinking, store.StateThinking, store.StateDone, store.StateNew},
			wantContains: []string{"1 waiting", "2 working", "1 done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			for i, state := range tt.states {
				seed(t, st, string(rune('a'+i)), state)
			}
			got := renderStatus(t.Context(), st)
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
		})
	}
}
