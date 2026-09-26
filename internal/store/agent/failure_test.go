package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetFailureReason pins that a reason lands on the agent it names, archived or not, and that an
// id belonging to no agent changes nothing.
func TestSetFailureReason(t *testing.T) {
	tests := []struct {
		name        string
		create      bool
		archive     bool
		id          func(created Agent) string
		wantChanged bool
	}{
		{
			name: "an id no agent holds",
			id: func(Agent) string {
				return uuid.NewString()
			},
		},
		{
			name: "a value that is not an id at all",
			id: func(Agent) string {
				return "not-an-id"
			},
		},
		{
			name:   "a live agent",
			create: true,
			id: func(created Agent) string {
				return created.ID
			},
			wantChanged: true,
		},
		{
			name:    "an archived agent",
			create:  true,
			archive: true,
			id: func(created Agent) string {
				return created.ID
			},
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			var created Agent
			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateFailedToCreate}))
				stored, err := st.Get(ctx, "agent")
				require.NoError(t, err)
				created = *stored
			}
			if tt.archive {
				_, err := st.Archive(ctx, []string{created.ID})
				require.NoError(t, err)
			}

			changed, err := st.SetFailureReason(ctx, tt.id(created), "worktree is dirty")
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			if !tt.create {
				return
			}
			after, err := st.GetByID(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, "worktree is dirty", after.FailureReason)
		})
	}
}
