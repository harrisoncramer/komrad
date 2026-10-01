package screen_test

import (
	"testing"
	"time"

	"komrad/internal/store"
	"komrad/internal/storetest"

	agentstore "komrad/internal/store/agent"
	screenstore "komrad/internal/store/screen"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpsert covers keeping one screen per agent.
func TestUpsert(t *testing.T) {
	tests := []struct {
		name  string
		saves []screenstore.UpsertParams
		want  screenstore.Screen
	}{
		{
			name: "the first save is kept",
			saves: []screenstore.UpsertParams{{
				Frame:  "❯ first",
				Width:  80,
				Height: 24,
			}},
			want: screenstore.Screen{
				Frame:  "❯ first",
				Width:  80,
				Height: 24,
			},
		},
		{
			name: "a later save replaces the frame and its size",
			saves: []screenstore.UpsertParams{
				{
					Frame:  "❯ first",
					Width:  80,
					Height: 24,
				},
				{
					Frame:  "❯ second",
					Width:  120,
					Height: 40,
				},
			},
			want: screenstore.Screen{
				Frame:  "❯ second",
				Width:  120,
				Height: 40,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
			before := time.Now().Add(-time.Minute)

			for _, save := range tt.saves {
				save.AgentID = agent.ID
				require.NoError(t, st.Screens.Upsert(t.Context(), save))
			}

			got, err := st.Screens.Get(t.Context(), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, agent.ID, got.AgentID)
			assert.Equal(t, tt.want.Frame, got.Frame)
			assert.Equal(t, tt.want.Width, got.Width)
			assert.Equal(t, tt.want.Height, got.Height)
			assert.True(t, got.CapturedAt.After(before))
		})
	}
}

// TestUpsertRefusesAScreenWithNoSize covers the size check.
func TestUpsertRefusesAScreenWithNoSize(t *testing.T) {
	st := storetest.Open(t)
	agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})

	err := st.Screens.Upsert(t.Context(), screenstore.UpsertParams{
		AgentID: agent.ID,
		Frame:   "❯",
		Width:   0,
		Height:  24,
	})

	require.Error(t, err)
}

// TestGet covers when no screen is found.
func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		agentID func(st *storetest.Stores) string
	}{
		{
			name:    "an id that is not an id",
			agentID: func(*storetest.Stores) string { return "not-an-id" },
		},
		{
			name: "an agent that never saved a screen",
			agentID: func(st *storetest.Stores) string {
				return st.SeedAgent(t, agentstore.Agent{Name: "quiet"}).ID
			},
		},
		{
			name: "an agent deleted after it saved one",
			agentID: func(st *storetest.Stores) string {
				agent := st.SeedAgent(t, agentstore.Agent{Name: "gone"})
				require.NoError(t, st.Screens.Upsert(t.Context(), screenstore.UpsertParams{
					AgentID: agent.ID,
					Frame:   "❯",
					Width:   80,
					Height:  24,
				}))
				_, err := st.Agents.Delete(t.Context(), []string{agent.ID})
				require.NoError(t, err)

				return agent.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)

			_, err := st.Screens.Get(t.Context(), tt.agentID(st))

			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}
