package agent

import (
	"testing"

	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActiveNameExists pins that a name is taken only by a live agent in the same workflow,
// so an archived one blocks nothing.
func TestActiveNameExists(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool

		elsewhere bool
		want      bool
	}{
		{
			name: "nobody has the name yet",
		},
		{
			name:    "only an archived agent holds it",
			create:  true,
			archive: true,
		},
		{
			name:      "a live agent in another workflow holds it",
			create:    true,
			elsewhere: true,
		},
		{
			name:   "a live agent holds it",
			create: true,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			workflowID := st.defaultWorkflow
			if tt.elsewhere {
				other, err := st.workflows.Create(ctx, workflowstore.Config{Name: "other"})
				require.NoError(t, err)
				workflowID = other.ID
			}

			if tt.create {
				require.NoError(t, st.Store.Create(ctx, Agent{
					Name:       "agent",
					State:      StateIdle,
					WorkflowID: workflowID,
				}))
			}
			if tt.archive {
				archived, err := st.GetInWorkflow(ctx, workflowID, "agent")
				require.NoError(t, err)
				require.NotNil(t, archived)
				_, archiveErr := st.Archive(ctx, []string{archived.ID})
				require.NoError(t, archiveErr)
			}

			exists, err := st.ActiveNameExists(ctx, st.defaultWorkflow, "agent")
			require.NoError(t, err)
			assert.Equal(t, tt.want, exists)
		})
	}
}

// TestListPageArchived pins that an agent never appears in both listings, since the panel
// would offer keys on a row that is gone and the archive would offer to destroy a live one.
func TestListPageArchived(t *testing.T) {
	tests := []struct {
		name      string
		archived  bool
		wantNames []string
	}{
		{
			name:      "the live listing leaves out what was archived",
			wantNames: []string{"live"},
		},
		{
			name:      "the archive listing is the archived rows and only those",
			archived:  true,
			wantNames: []string{"archived"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "archived", Pane: "%1", State: StateIdle}))
			require.NoError(t, st.archiveByName(ctx, "archived"))
			require.NoError(t, st.Create(ctx, Agent{Name: "live", Pane: "%2", State: StateIdle}))

			page, err := st.ListPage(ctx, 10, nil, tt.archived)
			require.NoError(t, err)

			names := make([]string, 0, len(page))
			for _, agent := range page {
				names = append(names, agent.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// TestGetByID pins that the lookup spans archived agents, whose id is the only handle they
// still answer to once a live agent has taken the name.
func TestGetByID(t *testing.T) {
	tests := []struct {
		name string

		lookFor func(created Agent) string
		archive bool
		wantHit bool
	}{
		{
			name:    "an id nobody holds finds nothing",
			lookFor: func(Agent) string { return uuid.NewString() },
		},
		{
			name:    "and neither does a string that is not an id",
			lookFor: func(Agent) string { return "not-an-id" },
		},
		{
			name:    "an empty id finds nothing rather than failing",
			lookFor: func(Agent) string { return "" },
		},
		{
			name:    "the id an agent holds finds it",
			lookFor: func(created Agent) string { return created.ID },
			wantHit: true,
		},
		{
			name:    "and still finds it once the agent is archived",
			lookFor: func(created Agent) string { return created.ID },
			archive: true,
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, created)

			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}

			got, err := st.GetByID(ctx, tt.lookFor(*created))

			if !tt.wantHit {
				assert.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, created.ID, got.ID)
			assert.Equal(t, "agent", got.Name)
		})
	}
}

// TestGetByIDCarriesMetadata covers that a restore hands the agent back with what it
// recorded about its own work, not just its stored fields.
func TestGetByIDCarriesMetadata(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	changed, err := st.SetMetadata(ctx, SetMetadataParams{
		ID:    created.ID,
		Key:   "pull_request",
		Value: xptr.Ptr("https://github.com/o/r/pull/1"),
	})
	require.NoError(t, err)
	require.True(t, changed)

	got, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Metadata, 1)
	assert.Equal(t, "pull_request", got.Metadata[0].Key)
	assert.Equal(t, "https://github.com/o/r/pull/1", got.Metadata[0].Value)
}

// TestListByID pins that a batch read says which of the ids it was given found nothing, so
// what it leaves out matters as much as what it returns.
func TestListByID(t *testing.T) {
	tests := []struct {
		name string

		create []string

		ids       func(seeded map[string]string) []string
		wantNames []string
	}{
		{
			name: "no ids at all",
			ids:  func(map[string]string) []string { return nil },
		},
		{
			name:   "an id nobody holds comes back missing rather than as an error",
			create: []string{"auth"},
			ids:    func(map[string]string) []string { return []string{uuid.NewString()} },
		},
		{
			name:   "a value that is not an id, which Postgres would refuse the array over",
			create: []string{"auth"},
			ids: func(seeded map[string]string) []string {
				return []string{"not-an-id", seeded["auth"]}
			},
			wantNames: []string{"auth"},
		},
		{
			name:   "the agents that were found, whatever else was asked for",
			create: []string{"auth", "api", "db"},
			ids: func(seeded map[string]string) []string {
				return []string{seeded["auth"], uuid.NewString(), seeded["db"]}
			},
			wantNames: []string{"auth", "db"},
		},
		{
			name:   "an archived agent, since an id is the handle one of those still answers to",
			create: []string{"auth"},
			ids: func(seeded map[string]string) []string {
				return []string{seeded["auth"]}
			},
			wantNames: []string{"auth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			seeded := make(map[string]string, len(tt.create))
			for _, name := range tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))

				created, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, created)
				seeded[name] = created.ID
			}

			got, err := st.ListByID(ctx, tt.ids(seeded))
			require.NoError(t, err)

			names := make([]string, 0, len(got))
			for _, agent := range got {
				names = append(names, agent.Name)
			}

			assert.ElementsMatch(t, tt.wantNames, names)
		})
	}
}

// TestListByIDCarriesMetadata pins the metadata coming back with a batch read, since a
// restore hands its agents straight to the panel that draws them.
func TestListByIDCarriesMetadata(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "auth", State: StateIdle}))
	require.NoError(t, st.Create(ctx, Agent{Name: "api", State: StateIdle}))

	auth, err := st.Get(ctx, "auth")
	require.NoError(t, err)
	require.NotNil(t, auth)
	api, err := st.Get(ctx, "api")
	require.NoError(t, err)
	require.NotNil(t, api)

	written, err := st.SetMetadata(ctx, SetMetadataParams{ID: auth.ID, Key: "pr", Value: xptr.Ptr("https://example.test/1")})
	require.NoError(t, err)
	require.True(t, written)

	got, err := st.ListByID(ctx, []string{auth.ID, api.ID})
	require.NoError(t, err)
	require.Len(t, got, 2)

	byID := make(map[string][]Metadata, len(got))
	for _, agent := range got {
		byID[agent.ID] = agent.Metadata
	}

	assert.Equal(t, []Metadata{{Key: "pr", Value: "https://example.test/1"}}, byID[auth.ID])
	assert.Empty(t, byID[api.ID], "an agent that recorded nothing carries nothing")
}
