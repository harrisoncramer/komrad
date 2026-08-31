package agent

import (
	"testing"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xptr"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateAndUpdate pins that an update rewrites the config row an agent owns rather than
// orphaning it behind a new id.
func TestCreateAndUpdate(t *testing.T) {
	tests := []struct {
		name    string
		initial Agent

		update    func(a *Agent)
		wantCwd   string
		wantFlags string
		wantPane  string
	}{
		{
			name:      "the fields a new agent was given are read back",
			initial:   Agent{Name: "agent", Pane: "%1", Cwd: "/work/api", Flags: "--model opus", Prompt: "do the thing", State: StateIdle},
			wantCwd:   "/work/api",
			wantFlags: "--model opus",
			wantPane:  "%1",
		},
		{
			name:      "an update rewrites the owned config row in place",
			initial:   Agent{Name: "agent", Pane: "%1", Cwd: "/work/api", Flags: "--model opus", State: StateIdle},
			update:    func(a *Agent) { a.Cwd = "/work/web"; a.Pane = "%3" },
			wantCwd:   "/work/web",
			wantFlags: "--model opus",
			wantPane:  "%3",
		},
		{
			name:      "an update leaves the fields it did not touch alone",
			initial:   Agent{Name: "agent", Pane: "%1", Cwd: "/work/api", Flags: "--model opus", State: StateIdle},
			update:    func(a *Agent) { a.State = StateThinking },
			wantCwd:   "/work/api",
			wantFlags: "--model opus",
			wantPane:  "%1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, tt.initial))
			stored, err := st.Get(ctx, tt.initial.Name)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.NotEmpty(t, stored.ID, "an id is minted for a caller that brought none")

			if tt.update != nil {
				updated := *stored
				tt.update(&updated)
				require.NoError(t, st.Update(ctx, updated))

				stored, err = st.Get(ctx, tt.initial.Name)
				require.NoError(t, err)
				require.NotNil(t, stored)
			}

			assert.Equal(t, tt.wantCwd, stored.Cwd)
			assert.Equal(t, tt.wantFlags, stored.Flags)
			assert.Equal(t, tt.wantPane, stored.Pane)

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, 1, "an update writes the same row rather than a second one")
		})
	}
}

// TestCreateWithActionOverride pins that an override of none reads back as an empty string
// rather than as nil, which is what tells the two apart.
func TestCreateWithActionOverride(t *testing.T) {
	tests := []struct {
		name  string
		agent Agent
	}{
		{
			name:  "no override reads back nil",
			agent: Agent{Name: "agent"},
		},
		{
			name:  "an override of none reads back an empty string, not nil",
			agent: Agent{Name: "agent", SetupAction: xptr.Ptr(""), CleanupAction: xptr.Ptr("")},
		},
		{
			name:  "a named override reads back the name",
			agent: Agent{Name: "agent", SetupAction: xptr.Ptr("install-deps"), CleanupAction: xptr.Ptr("drop-worktree")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			ctx := t.Context()

			require.NoError(t, f.Create(ctx, tt.agent))
			stored, err := f.Get(ctx, tt.agent.Name)
			require.NoError(t, err)
			require.NotNil(t, stored)

			assert.Equal(t, tt.agent.SetupAction, stored.SetupAction)
			assert.Equal(t, tt.agent.CleanupAction, stored.CleanupAction)
		})
	}
}

// TestSpawnedByAgentID pins that an agent a person started reads back a nil parent rather
// than a sentinel standing in for nobody.
func TestSpawnedByAgentID(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "parent"}))
	parent, err := f.Get(ctx, "parent")
	require.NoError(t, err)
	require.NotNil(t, parent)

	tests := []struct {
		name           string
		spawnedByAgent bool
	}{
		{
			name: "an agent a person started records no parent",
		},
		{
			name:           "an agent another agent started records its id",
			spawnedByAgent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := Agent{Name: "child-" + uuid.NewString()}
			if tt.spawnedByAgent {
				child.SpawnedByAgentID = &parent.ID
			}

			require.NoError(t, f.Create(ctx, child))
			stored, err := f.Get(ctx, child.Name)
			require.NoError(t, err)
			require.NotNil(t, stored)

			if !tt.spawnedByAgent {
				assert.Nil(t, stored.SpawnedByAgentID)

				return
			}

			require.NotNil(t, stored.SpawnedByAgentID)
			assert.Equal(t, parent.ID, *stored.SpawnedByAgentID)
		})
	}
}

// TestDeleteNullsSpawnedByAgentID pins that deleting a parent nulls the reference rather
// than taking the work its children did with it.
func TestDeleteNullsSpawnedByAgentID(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "parent"}))
	parent, err := f.Get(ctx, "parent")
	require.NoError(t, err)
	require.NotNil(t, parent)

	require.NoError(t, f.Create(ctx, Agent{Name: "child", SpawnedByAgentID: &parent.ID}))
	child, err := f.Get(ctx, "child")
	require.NoError(t, err)
	require.NotNil(t, child)
	require.NotNil(t, child.SpawnedByAgentID)

	_, deleteErr := f.Delete(ctx, []string{parent.ID})

	require.NoError(t, deleteErr)

	stillThere, err := f.GetByID(ctx, child.ID)
	require.NoError(t, err)
	require.NotNil(t, stillThere, "the child survives its parent's deletion")
	assert.Nil(t, stillThere.SpawnedByAgentID, "the reference is nulled rather than left dangling")
}

// TestDeleteTakesMetadata pins that a hard delete carries an agent's facts with it, since
// nothing deletes them by hand and a missing cascade would leave rows keyed to a dead id.
func TestDeleteTakesMetadata(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: "pr", Value: xptr.Ptr("https://github.com/o/r/pull/1")})
	require.NoError(t, err)
	require.True(t, changed)

	_, deleteErr := st.Delete(ctx, []string{created.ID})

	require.NoError(t, deleteErr)

	orphaned, err := models.New(st.db).ListAgentMetadata(ctx, []string{created.ID})
	require.NoError(t, err)
	assert.Empty(t, orphaned)
}

// TestRename pins that a rename onto a live agent's name comes back as the caller's mistake
// while a rename onto a name only an archived row holds is allowed.
func TestRename(t *testing.T) {
	tests := []struct {
		name string

		target string

		archiveOther bool
		to           string
		wantErr      error

		wantRenamed bool
	}{
		{
			name:   "an id no agent holds touches nothing",
			target: "00000000-0000-0000-0000-0000000000ff",
			to:     "free",
		},
		{
			name:   "an id that is not an id touches nothing",
			target: "not-a-uuid",
			to:     "free",
		},
		{
			name:    "a name a live agent already holds",
			to:      "taken",
			wantErr: store.ErrNameTaken,
		},
		{
			name:         "a name only an archived row holds is free again",
			archiveOther: true,
			to:           "taken",
			wantRenamed:  true,
		},
		{
			name:        "the name it already answers to",
			to:          "agent",
			wantRenamed: true,
		},
		{
			name:        "a name nobody holds",
			to:          "renamed",
			wantRenamed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateIdle}))
			require.NoError(t, st.Create(ctx, Agent{Name: "taken", Pane: "%2", State: StateIdle}))
			if tt.archiveOther {
				require.NoError(t, st.archiveByName(ctx, "taken"))
			}

			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, created)

			target := tt.target
			if target == "" {
				target = created.ID
			}

			renamed, err := st.Rename(ctx, target, tt.to)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, renamed)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRenamed, renamed)

			if !tt.wantRenamed {
				return
			}

			under, err := st.Get(ctx, tt.to)
			require.NoError(t, err)
			require.NotNil(t, under)
			assert.Equal(t, created.ID, under.ID, "the rename kept the agent's id")
		})
	}
}

// TestDelete covers the two ways an agent leaves the listing, which differ in
// whether the row survives to be restored later.
func TestDelete(t *testing.T) {
	tests := []struct {
		name string

		hard     bool
		wantLive int
		wantAll  int
	}{
		{
			name:    "a hard delete drops the row",
			hard:    true,
			wantAll: 0,
		},
		{
			name:    "an archive keeps it out of sight but on disk",
			wantAll: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "alpha", Pane: "%1", State: StateIdle}))

			if tt.hard {
				created, err := st.Get(ctx, "alpha")
				require.NoError(t, err)
				require.NotNil(t, created)

				_, deleteErr := st.Delete(ctx, []string{created.ID})

				require.NoError(t, deleteErr)
			} else {
				require.NoError(t, st.archiveByName(ctx, "alpha"))
			}

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, tt.wantLive)

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantAll)
			if !tt.hard {
				require.Len(t, all, 1)
				assert.NotNil(t, all[0].ArchivedAt, "an archived row records when it was archived")
			}
		})
	}
}

// TestDeleteByIDPicksTheArchivedRow pins that a hard delete keys on the id, since an
// archived agent can share a name with a live one.
func TestDeleteByIDPicksTheArchivedRow(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "shared", Pane: "%1", State: StateIdle}))
	first, err := st.Get(ctx, "shared")
	require.NoError(t, err)
	require.NotNil(t, first)

	require.NoError(t, st.archiveByName(ctx, "shared"))
	require.NoError(t, st.Create(ctx, Agent{Name: "shared", Pane: "%2", State: StateIdle}))

	_, deleteErr := st.Delete(ctx, []string{first.ID})

	require.NoError(t, deleteErr)

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "only the archived row was destroyed")
	assert.NotEqual(t, first.ID, all[0].ID, "the live agent holding the same name is untouched")
	assert.Equal(t, "%2", all[0].Pane)
}

// TestDeleteInBatches covers the hard delete over a list, which is what emptying a
// selection out of the archive does.
func TestDeleteInBatches(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	ids := make([]string, 0, 3)
	for _, name := range []string{"auth", "api", "db"} {
		require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))

		created, err := st.Get(ctx, name)
		require.NoError(t, err)
		require.NotNil(t, created)
		ids = append(ids, created.ID)
	}

	_, deleteErr := st.Delete(ctx, []string{ids[0], "not-an-id", ids[2]})

	require.NoError(t, deleteErr)

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the id that could hold no row took nothing with it")
	assert.Equal(t, "api", all[0].Name)
}
