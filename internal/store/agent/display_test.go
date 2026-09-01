package agent

import (
	"fmt"
	"testing"

	"weave/lib/xptr"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetTags pins that a tag write reports what it landed on, so a caller is never left
// believing they tagged agents that show no tag.
func TestSetTags(t *testing.T) {
	tests := []struct {
		name       string
		create     int
		archive    bool
		ids        func(created []Agent) []string
		before     *string
		tag        *string
		wantTagged int
	}{
		{
			name: "an id no agent holds",
			ids:  func([]Agent) []string { return []string{uuid.NewString()} },
			tag:  xptr.Ptr("●"),
		},
		{
			name: "a value that is not an id at all",
			ids:  func([]Agent) []string { return []string{"not-an-id"} },
			tag:  xptr.Ptr("●"),
		},
		{
			name:    "an archived agent, which has no listing to be tagged in",
			create:  1,
			archive: true,
			ids:     func(created []Agent) []string { return []string{created[0].ID} },
			tag:     xptr.Ptr("●"),
		},
		{
			name:       "a value that is not an id, which must not take the agent beside it down",
			create:     1,
			ids:        func(created []Agent) []string { return []string{"not-an-id", created[0].ID} },
			tag:        xptr.Ptr("●"),
			wantTagged: 1,
		},
		{
			name:       "an id nobody holds beside one somebody does",
			create:     1,
			ids:        func(created []Agent) []string { return []string{created[0].ID, uuid.NewString()} },
			tag:        xptr.Ptr("●"),
			wantTagged: 1,
		},
		{
			name:       "tagging a live agent",
			create:     1,
			ids:        agentIDs,
			tag:        xptr.Ptr("●"),
			wantTagged: 1,
		},
		{
			name:       "tagging several at once",
			create:     3,
			ids:        agentIDs,
			tag:        xptr.Ptr("●"),
			wantTagged: 3,
		},
		{
			name:       "moving several agents from one tag to another",
			create:     2,
			ids:        agentIDs,
			before:     xptr.Ptr("●"),
			tag:        xptr.Ptr("✓"),
			wantTagged: 2,
		},
		{
			name:       "clearing the tags several agents carry",
			create:     2,
			ids:        agentIDs,
			before:     xptr.Ptr("●"),
			wantTagged: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created := make([]Agent, 0, tt.create)
			for i := range tt.create {
				name := fmt.Sprintf("agent-%d", i)
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))

				got, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, got)
				created = append(created, *got)
			}
			if tt.before != nil {
				tagged, err := st.SetTags(ctx, agentIDs(created), tt.before)
				require.NoError(t, err)
				require.Equal(t, len(created), tagged)
			}
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, created[0].Name))
			}

			tagged, err := st.SetTags(ctx, tt.ids(created), tt.tag)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTagged, tagged)

			carrying := 0
			for _, agent := range created {
				after, err := st.GetByID(ctx, agent.ID)
				require.NoError(t, err)
				require.NotNil(t, after)

				if assert.ObjectsAreEqual(tt.tag, after.Tag) {
					carrying++
				}
			}

			assert.Equal(t, tt.wantTagged, carrying)
		})
	}
}

// TestSetHidden pins that a hide write reports what it landed on, so the panel never draws
// a row the caller believes it took away.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name        string
		create      bool
		archive     bool
		id          func(created Agent) string
		before      bool
		hidden      bool
		wantChanged bool
	}{
		{
			name:   "an id no agent holds",
			id:     func(Agent) string { return uuid.NewString() },
			hidden: true,
		},
		{
			name:   "a value that is not an id at all",
			id:     func(Agent) string { return "not-an-id" },
			hidden: true,
		},
		{
			name:    "an archived agent, which has no listing to be hidden from",
			create:  true,
			archive: true,
			id:      func(created Agent) string { return created.ID },
			hidden:  true,
		},
		{
			name:        "hiding a live agent",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			hidden:      true,
			wantChanged: true,
		},
		{
			name:        "bringing a hidden agent back",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			before:      true,
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			var created Agent
			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
				got, err := st.Get(ctx, "agent")
				require.NoError(t, err)
				require.NotNil(t, got)
				created = *got
				assert.False(t, got.Hidden, "an agent starts out listed")
			}
			if tt.before {
				changed, err := st.SetHidden(ctx, created.ID, true)
				require.NoError(t, err)
				require.True(t, changed)
			}
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}

			changed, err := st.SetHidden(ctx, tt.id(created), tt.hidden)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			if !tt.wantChanged {
				return
			}

			after, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, tt.hidden, after.Hidden)
		})
	}
}
