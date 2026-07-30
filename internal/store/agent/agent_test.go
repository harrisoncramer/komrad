package agent

import (
	"context"
	"fmt"
	"testing"

	"claude-agents/db/models"
	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	schedulestore "claude-agents/internal/store/schedule"
	workflowstore "claude-agents/internal/store/workflow"
	"claude-agents/lib/xptr"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "colon becomes a dash",
			in:   "a:b",
			want: "a-b",
		},
		{
			name: "dot becomes a dash",
			in:   "v1.0",
			want: "v1-0",
		},
		{
			name: "space becomes a dash",
			in:   "my agent",
			want: "my-agent",
		},
		{
			name: "a mix is fully sanitized",
			in:   "My Agent: v1.0",
			want: "My-Agent--v1-0",
		},
		{
			name: "an already-clean name is unchanged",
			in:   "clean-name",
			want: "clean-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeName(tt.in))
		})
	}
}

// TestCreateAndUpdate covers what an agent carries in and out of the store,
// including the config row it owns. An update has to rewrite that row rather than
// leave the old one orphaned behind a new id.
func TestCreateAndUpdate(t *testing.T) {
	tests := []struct {
		name    string
		initial Agent
		// update, when set, is applied to the stored agent and written back, which
		// is the path a rename or a relaunch takes.
		update    func(a *Agent)
		wantCwd   string
		wantFlags string
		wantPane  string
	}{
		{
			name:      "the fields a new agent was given are read back",
			initial:   Agent{Name: "agent", Pane: "%1", Cwd: "/work/api", Flags: "--model opus", Prompt: "do the thing", State: StateNew},
			wantCwd:   "/work/api",
			wantFlags: "--model opus",
			wantPane:  "%1",
		},
		{
			name:      "an update rewrites the owned config row in place",
			initial:   Agent{Name: "agent", Pane: "%1", Cwd: "/work/api", Flags: "--model opus", State: StateNew},
			update:    func(a *Agent) { a.Cwd = "/work/web"; a.Pane = "%3" },
			wantCwd:   "/work/web",
			wantFlags: "--model opus",
			wantPane:  "%3",
		},
		{
			name:      "an update leaves the fields it did not touch alone",
			initial:   Agent{Name: "agent", Pane: "%1", Cwd: "/work/api", Flags: "--model opus", State: StateNew},
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

// TestArchiveAndReuse covers who a name belongs to over an agent's life. An
// archived row keeps its name, so reusing it has to create a fresh row rather
// than resurrect the dead one, and state events keyed by name must land on the
// live row.
func TestArchiveAndReuse(t *testing.T) {
	tests := []struct {
		name    string
		archive bool
		// reuse writes a second agent under the same name after the archive.
		reuse bool
		// wantVisible says Get should still find the name.
		wantVisible bool
		wantLive    int
		wantAll     int
	}{
		{
			name:        "a live agent is visible and listed",
			wantVisible: true,
			wantLive:    1,
			wantAll:     1,
		},
		{
			name:     "an archived agent is hidden but retained",
			archive:  true,
			wantLive: 0,
			wantAll:  1,
		},
		{
			name:        "reusing an archived name creates a fresh row",
			archive:     true,
			reuse:       true,
			wantVisible: true,
			wantLive:    1,
			wantAll:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
			first, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, first)

			if tt.archive {
				require.NoError(t, st.Archive(ctx, "agent"))
			}
			if tt.reuse {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%2", State: StateNew}))
			}

			got, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			if !tt.wantVisible {
				assert.Nil(t, got, "an archived row is hidden from Get")
			} else {
				require.NotNil(t, got)
			}

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, tt.wantLive)

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantAll, "archived rows are retained")

			if !tt.reuse {
				return
			}

			assert.NotEqual(t, first.ID, got.ID, "a reused name must not resurrect the archived row")
			assert.Equal(t, StateNew, got.State)
			assert.Equal(t, "%2", got.Pane)

			require.NoError(t, st.SetStateByID(ctx, got.ID, StateWaiting))

			live, err = st.List(ctx)
			require.NoError(t, err)
			require.Len(t, live, 1)
			assert.Equal(t, StateWaiting, live[0].State, "the live row takes the new state")

			all, err = st.ListAll(ctx)
			require.NoError(t, err)
			for _, agent := range all {
				if agent.ID == first.ID {
					assert.Equal(t, StateThinking, agent.State, "the archived row is left frozen")
				}
			}
		})
	}
}

// TestSetStateByID guards the rule that a state write never creates a row. The
// reconciler decides a state from a listing it took moments earlier, so it can
// name an agent that has since been deleted, and that write has to find nothing
// rather than put the row back.
func TestSetStateByID(t *testing.T) {
	tests := []struct {
		name string
		// target, when set, is written instead of the created agent's own id.
		target string
		// wantState is what the created agent should be sitting in afterwards.
		wantState string
	}{
		{
			name:      "an id no agent holds",
			target:    "00000000-0000-0000-0000-0000000000ff",
			wantState: StateNew,
		},
		{
			name:      "an id that is not an id",
			target:    "not-a-uuid",
			wantState: StateNew,
		},
		{
			name:      "the agent's own id",
			wantState: StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, created)

			target := tt.target
			if target == "" {
				target = created.ID
			}

			require.NoError(t, st.SetStateByID(ctx, target, StateThinking))

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1, "a state write never inserts")
			assert.Equal(t, tt.wantState, all[0].State)
		})
	}
}

// TestSetMark covers marking, changing a mark, clearing one, and who may carry one.
// A write that silently landed on nothing would leave the caller believing they had
// marked an agent that shows no mark.
func TestSetMark(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		id      func(created Agent) string
		// before is put on the agent ahead of the write, so a row can start from a
		// mark rather than from nothing.
		before      *string
		mark        *string
		wantChanged bool
	}{
		{
			name: "an id no agent holds",
			id:   func(Agent) string { return uuid.NewString() },
			mark: xptr.Ptr("●"),
		},
		{
			name: "a value that is not an id at all",
			id:   func(Agent) string { return "not-an-id" },
			mark: xptr.Ptr("●"),
		},
		{
			name:    "an archived agent, which has no listing to be marked in",
			create:  true,
			archive: true,
			id:      func(created Agent) string { return created.ID },
			mark:    xptr.Ptr("●"),
		},
		{
			name:        "marking a live agent",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			mark:        xptr.Ptr("●"),
			wantChanged: true,
		},
		{
			name:        "moving an agent from one mark to another",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			before:      xptr.Ptr("●"),
			mark:        xptr.Ptr("✓"),
			wantChanged: true,
		},
		{
			name:        "clearing a live agent's mark",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			before:      xptr.Ptr("●"),
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			var created Agent
			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
				got, err := st.Get(ctx, "agent")
				require.NoError(t, err)
				require.NotNil(t, got)
				created = *got
			}
			if tt.before != nil {
				changed, err := st.SetMark(ctx, created.ID, tt.before)
				require.NoError(t, err)
				require.True(t, changed)
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, "agent"))
			}

			changed, err := st.SetMark(ctx, tt.id(created), tt.mark)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			if !tt.wantChanged {
				return
			}

			after, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, tt.mark, after.Mark)
		})
	}
}

// TestSetHidden covers hiding an agent, bringing it back, and who may be hidden at
// all. A write that landed on nothing would leave the panel drawing a row the caller
// believes it took away.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		id      func(created Agent) string
		// before is written ahead of the call, so unhiding has something to undo.
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
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
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
				require.NoError(t, st.Archive(ctx, "agent"))
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

// TestSetMetadata covers what an agent may record against itself. The write has to
// land on exactly one key, since an agent recording a second fact must not drop the
// first, and it has to refuse an id belonging to no live agent rather than report a
// success the agent would go on believing.
func TestSetMetadata(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		id      func(created Agent) string
		// before is written ahead of the call, so a row can start from facts it
		// already recorded rather than from nothing.
		before      []Metadata
		key         string
		value       *string
		wantChanged bool
		wantStored  []Metadata
	}{
		{
			name:  "an id no agent holds",
			id:    func(Agent) string { return uuid.NewString() },
			key:   "pr",
			value: xptr.Ptr("https://github.com/o/r/pull/1"),
		},
		{
			name:  "a value that is not an id at all",
			id:    func(Agent) string { return "not-an-id" },
			key:   "pr",
			value: xptr.Ptr("https://github.com/o/r/pull/1"),
		},
		{
			name:    "an archived agent, which has no listing to show metadata in",
			create:  true,
			archive: true,
			id:      func(created Agent) string { return created.ID },
			key:     "pr",
			value:   xptr.Ptr("https://github.com/o/r/pull/1"),
		},
		{
			name:   "clearing a key the agent never held",
			create: true,
			id:     func(created Agent) string { return created.ID },
			key:    "pr",
		},
		{
			name:        "recording a fact",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			key:         "pr",
			value:       xptr.Ptr("https://github.com/o/r/pull/1"),
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
		},
		{
			name:   "revising a fact leaves the agent's other keys standing",
			create: true,
			id:     func(created Agent) string { return created.ID },
			before: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
			key:         "pr",
			value:       xptr.Ptr("https://github.com/o/r/pull/2"),
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
				{Key: "pr", Value: "https://github.com/o/r/pull/2"},
			},
		},
		{
			name:   "clearing one key leaves the rest",
			create: true,
			id:     func(created Agent) string { return created.ID },
			before: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
			key:         "pr",
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			var created Agent
			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
				got, err := st.Get(ctx, "agent")
				require.NoError(t, err)
				require.NotNil(t, got)
				created = *got
			}
			for _, entry := range tt.before {
				changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: entry.Key, Value: xptr.Ptr(entry.Value)})
				require.NoError(t, err)
				require.True(t, changed)
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, "agent"))
			}

			changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: tt.id(created), Key: tt.key, Value: tt.value})
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			if !tt.create || tt.archive {
				return
			}

			after, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, tt.wantStored, after.Metadata)
		})
	}
}

// TestSetMetadataCeiling pins the cap on how many facts one agent may record. The
// keys are the agent's own invention, so nothing else bounds them, but revising a
// fact it already recorded has to keep working however full it is.
func TestSetMetadataCeiling(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	for i := range maxMetadataKeys {
		changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: fmt.Sprintf("key-%d", i), Value: xptr.Ptr("v")})
		require.NoError(t, err)
		require.True(t, changed)
	}

	_, err = st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: "one-too-many", Value: xptr.Ptr("v")})
	require.ErrorIs(t, err, ErrTooManyMetadataKeys)

	changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: "key-0", Value: xptr.Ptr("revised")})
	require.NoError(t, err, "revising a key the agent already holds is not a new key")
	assert.True(t, changed)
}

// TestListPageHydratesMetadata pins that a listing carries each agent's own facts and
// nobody else's, in one pass over the page. The panel renders straight off the
// listing, so metadata arriving only from a single-agent read would leave the info
// view empty.
func TestListPageHydratesMetadata(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	ids := make(map[string]string, 2)
	for _, name := range []string{"first", "second"} {
		require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateNew}))
		created, err := st.Get(ctx, name)
		require.NoError(t, err)
		require.NotNil(t, created)
		ids[name] = created.ID
	}

	// Written out of alphabetical order, so the ordering below is the query's rather
	// than an accident of insertion.
	for key, value := range map[string]string{"pr": "https://github.com/o/r/pull/1", "branch": "harry/x"} {
		changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: ids["first"], Key: key, Value: xptr.Ptr(value)})
		require.NoError(t, err)
		require.True(t, changed)
	}

	listed, err := st.ListPage(ctx, 10, nil, false)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	byName := make(map[string][]Metadata, len(listed))
	for _, agent := range listed {
		byName[agent.Name] = agent.Metadata
	}

	assert.Equal(t, []Metadata{
		{Key: "branch", Value: "harry/x"},
		{Key: "pr", Value: "https://github.com/o/r/pull/1"},
	}, byName["first"], "ordered by key, whatever order they were written in")
	assert.Empty(t, byName["second"], "an agent that recorded nothing carries nothing")
}

// TestDeleteTakesMetadata pins that a hard delete carries an agent's facts with it.
// They hang off the agent by a cascading key, so nothing deletes them by hand and a
// missing cascade would leave rows keyed to an id that no longer exists.
func TestDeleteTakesMetadata(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: "pr", Value: xptr.Ptr("https://github.com/o/r/pull/1")})
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, st.Delete(ctx, created.ID))

	orphaned, err := models.New(st.db).ListAgentMetadata(ctx, []string{created.ID})
	require.NoError(t, err)
	assert.Empty(t, orphaned)
}

// TestSetStateByIDKeepsTheMark pins that a state write is only a state write. The
// mark belongs to the user and the reconciler writes state constantly, so the two
// share a row without touching each other.
func TestSetStateByIDKeepsTheMark(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	changed, err := st.SetMark(ctx, created.ID, xptr.Ptr("●"))
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateWaiting))

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, StateWaiting, after.State)
	assert.Equal(t, "●", *after.Mark, "a state write leaves the user's mark standing")
}

// TestRename covers renaming by id, including the collision. A name is unique
// among live agents only, so a rename onto one that a live agent holds has to come
// back as the caller's mistake rather than as a driver error the handler would have
// to read, and a rename onto a name only an archived row holds has to be allowed.
func TestRename(t *testing.T) {
	tests := []struct {
		name string
		// target, when set, is renamed instead of the live agent that was created.
		target string
		// archiveOther archives the second agent, freeing the name it held.
		archiveOther bool
		to           string
		wantErr      error
		// wantRenamed says a row was actually touched.
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

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateNew}))
			require.NoError(t, st.Create(ctx, Agent{Name: "taken", Pane: "%2", State: StateNew}))
			if tt.archiveOther {
				require.NoError(t, st.Archive(ctx, "taken"))
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

// TestActiveNameExists covers who owns a name. The unique index is scoped to
// live rows, so an archived agent keeps its name without blocking a new one.
func TestActiveNameExists(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		want    bool
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
			name:   "a live agent holds it",
			create: true,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew}))
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, "agent"))
			}

			exists, err := st.ActiveNameExists(ctx, "agent")
			require.NoError(t, err)
			assert.Equal(t, tt.want, exists)
		})
	}
}

// TestRestore mirrors the manager's restore ordering at the store layer:
// undelete by id, then upsert the same id. It must not collide on the primary
// key, and what the agent belonged to has to survive the round trip.
func TestRestore(t *testing.T) {
	tests := []struct {
		name string
		// withWorkflow files the agent under a workflow first, since a restore that
		// dropped the assignment would strand it at the root.
		withWorkflow bool
		// hidden takes the agent out of the list before it is archived, so the restore
		// has a flag to clear.
		hidden bool
	}{
		{
			name: "a restored agent keeps its id and its conversation",
		},
		{
			name:         "and the workflow it belonged to",
			withWorkflow: true,
		},
		{
			name:   "one hidden before it was archived comes back listed",
			hidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			workflows := st.workflows
			ctx := t.Context()

			agent := Agent{Name: "beta", State: StateThinking, ClaudeSessionID: "sess-1"}
			if tt.withWorkflow {
				workflow, err := workflows.Create(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				agent.WorkflowID = workflow.ID
			}

			require.NoError(t, st.Create(ctx, agent))
			original, err := st.Get(ctx, "beta")
			require.NoError(t, err)
			require.NotNil(t, original)
			if tt.hidden {
				changed, err := st.SetHidden(ctx, original.ID, true)
				require.NoError(t, err)
				require.True(t, changed)
			}
			require.NoError(t, st.Archive(ctx, "beta"))

			require.NoError(t, st.Restore(ctx, original.ID))
			restored := *original
			restored.State = StateNew
			restored.Pane = "%9"
			require.NoError(t, st.Update(ctx, restored), "an update on restore must not collide")

			got, err := st.Get(ctx, "beta")
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, original.ID, got.ID, "a restored row keeps its id")
			assert.Equal(t, StateNew, got.State)
			assert.Equal(t, "sess-1", got.ClaudeSessionID, "restore preserves the conversation")
			assert.False(t, got.Hidden, "a restored agent comes back to a row")

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, 1, "a single row after restore")

			assert.Equal(t, original.WorkflowID, got.WorkflowID, "restore preserves the workflow")
		})
	}
}

// TestDelete covers the two ways an agent leaves the listing, which differ in
// whether the row survives to be restored later.
func TestDelete(t *testing.T) {
	tests := []struct {
		name string
		// hard drops the row outright rather than archiving it.
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

			require.NoError(t, st.Create(ctx, Agent{Name: "alpha", Pane: "%1", State: StateNew}))

			if tt.hard {
				created, err := st.Get(ctx, "alpha")
				require.NoError(t, err)
				require.NotNil(t, created)

				require.NoError(t, st.Delete(ctx, created.ID))
			} else {
				require.NoError(t, st.Archive(ctx, "alpha"))
			}

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, tt.wantLive)

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantAll)
		})
	}
}

// TestListPageDeleted covers the two sets a listing can ask for. They are what
// the panel and its trash each show, and an agent in one must never appear in
// the other: the tree would offer actions on a row that is gone, and the trash
// would offer to destroy one that is running.
func TestListPageDeleted(t *testing.T) {
	tests := []struct {
		name      string
		deleted   bool
		wantNames []string
	}{
		{
			name:      "the live listing leaves out what was deleted",
			wantNames: []string{"live"},
		},
		{
			name:      "the trash listing is the deleted rows and only those",
			deleted:   true,
			wantNames: []string{"archived"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "archived", Pane: "%1", State: StateNew}))
			require.NoError(t, st.Archive(ctx, "archived"))
			require.NoError(t, st.Create(ctx, Agent{Name: "live", Pane: "%2", State: StateNew}))

			page, err := st.ListPage(ctx, 10, nil, tt.deleted)
			require.NoError(t, err)

			names := make([]string, 0, len(page))
			for _, agent := range page {
				names = append(names, agent.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// TestDeleteByIDPicksTheArchivedRow covers why a hard delete keys on the id. The
// trash deletes through it, and the rows there are exactly the ones a name cannot
// tell apart: the unique index holds among live rows only, so an archived agent
// can share a name with a live one.
func TestDeleteByIDPicksTheArchivedRow(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "shared", Pane: "%1", State: StateNew}))
	first, err := st.Get(ctx, "shared")
	require.NoError(t, err)
	require.NotNil(t, first)

	require.NoError(t, st.Archive(ctx, "shared"))
	require.NoError(t, st.Create(ctx, Agent{Name: "shared", Pane: "%2", State: StateNew}))

	require.NoError(t, st.Delete(ctx, first.ID))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "only the archived row was destroyed")
	assert.NotEqual(t, first.ID, all[0].ID, "the live agent holding the same name is untouched")
	assert.Equal(t, "%2", all[0].Pane)
}

// TestGetByIdempotencyKey covers the lookup a retried launch makes. It spans
// archived agents on purpose: the key exists so a retry finds what it already
// created, and an agent archived since is still that agent.
func TestGetByIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		archive bool
		lookFor string
		wantHit bool
	}{
		{
			name:    "the key an agent holds finds it",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			lookFor: "schedule:s1:2026-07-28T09:00:00Z",
			wantHit: true,
		},
		{
			name:    "and still finds it after the agent is archived",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			archive: true,
			lookFor: "schedule:s1:2026-07-28T09:00:00Z",
			wantHit: true,
		},
		{
			name:    "a key nobody holds finds nothing",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			lookFor: "schedule:s1:2026-07-29T09:00:00Z",
		},
		{
			name:    "an empty key is not a claim, so it matches nothing",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			lookFor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateNew, IdempotencyKey: &tt.key}))
			if tt.archive {
				require.NoError(t, st.Archive(ctx, "agent"))
			}

			got, err := st.GetByIdempotencyKey(ctx, tt.lookFor)
			require.NoError(t, err)

			if !tt.wantHit {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Equal(t, "agent", got.Name)
			require.NotNil(t, got.IdempotencyKey)
			assert.Equal(t, tt.key, *got.IdempotencyKey)
		})
	}
}

// TestIdempotencyKeyIsUnique covers the index behind the lookup. Two agents
// claiming one key is the duplicate the whole mechanism exists to stop, while
// any number of agents claiming none is the ordinary case.
func TestIdempotencyKeyIsUnique(t *testing.T) {
	tests := []struct {
		name     string
		first    *string
		second   *string
		wantFail bool
	}{
		{
			name:     "the same key twice is refused",
			first:    xptr.Ptr("launch-1"),
			second:   xptr.Ptr("launch-1"),
			wantFail: true,
		},
		{
			name:   "two different keys are fine",
			first:  xptr.Ptr("launch-1"),
			second: xptr.Ptr("launch-2"),
		},
		{
			name: "so are two agents claiming no key at all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "first", State: StateNew, IdempotencyKey: tt.first}))

			err := st.Create(ctx, Agent{Name: "second", State: StateNew, IdempotencyKey: tt.second})
			if tt.wantFail {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestCountLiveForSchedule covers the count that stops a schedule lapping
// itself. An agent working or waiting on a confirmation counts, since that is
// work still owed rather than work done. One never seen working does not, since
// nothing will ever move it and the schedule would be blocked for good.
func TestCountLiveForSchedule(t *testing.T) {
	tests := []struct {
		name string
		// states are the agents the schedule has already produced.
		states []string
		// archive hides every one of them, as deleting from the panel would.
		archive bool
		// lookElsewhere counts against a schedule that produced none of them.
		lookElsewhere bool
		want          int64
	}{
		{
			name: "a schedule that has produced nothing has nothing running",
		},
		{
			name:   "an agent still thinking counts",
			states: []string{StateThinking},
			want:   1,
		},
		{
			name:   "so does one waiting on a prompt, which is unfinished work",
			states: []string{StateWaiting},
			want:   1,
		},
		{
			// The state a launch that never got going keeps for good, since only an
			// agent observed working is ever moved off it.
			name:   "one that has never been seen working does not",
			states: []string{StateNew},
			want:   0,
		},
		{
			name:   "a finished one does not",
			states: []string{StateDone},
			want:   0,
		},
		{
			name:   "several unfinished agents are all counted",
			states: []string{StateThinking, StateWaiting, StateDone},
			want:   2,
		},
		{
			name:    "an archived agent is not running, whatever state it stopped in",
			states:  []string{StateThinking},
			archive: true,
			want:    0,
		},
		{
			name:          "another schedule's agents are not this one's",
			states:        []string{StateThinking},
			lookElsewhere: true,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			schedule := seedSchedule(t, st)
			for i, state := range tt.states {
				name := fmt.Sprintf("agent-%d", i)
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: state, ScheduleID: &schedule}))
				if tt.archive {
					require.NoError(t, st.Archive(ctx, name))
				}
			}

			target := schedule
			if tt.lookElsewhere {
				target = seedSchedule(t, st)
			}

			count, err := st.CountLiveForSchedule(ctx, target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, count)
		})
	}
}

// fixture is the agent store under test plus the workflow store the tests seed
// through. Every agent belongs to a workflow, so one has to exist before an
// agent can be written. The two stay separate stores, since this package never
// reaches into the workflows table itself.
type fixture struct {
	*Store
	db        store.DBTX
	workflows *workflowstore.Store
	// defaultWorkflow holds any agent a test writes without naming one, which is
	// most of them: they are about names, states, and pages rather than about
	// where an agent belongs.
	defaultWorkflow string
}

// Create fills in the default workflow when the test did not name one.
func (f *fixture) Create(ctx context.Context, agent Agent) error {
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.defaultWorkflow
	}

	return f.Store.Create(ctx, agent)
}

// open builds the stores over a throwaway database, with one workflow ready to
// hold whatever the test writes. The tests run against real Postgres because
// most of what this package does is SQL: the partial unique index on live names,
// the keyset page, the generated ids.
func open(t *testing.T) *fixture {
	t.Helper()
	p := pool(t)

	workflows := workflowstore.New(p.DB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{Store: New(p.DB()), db: p.DB(), workflows: workflows, defaultWorkflow: created.ID}
}

// pool opens the database itself, for the few tests that also need another
// domain's store or a transaction.
func pool(t *testing.T) *store.Pool {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return p
}

// seedSchedule inserts a schedule and returns its id, for the counts that are
// about which schedule produced an agent rather than about the schedule itself.
func seedSchedule(t *testing.T, st *fixture) string {
	t.Helper()

	created, err := schedulestore.New(st.db).Create(t.Context(), schedulestore.Config{
		Name:       "schedule-" + uuid.NewString(),
		Cron:       "0 9 * * *",
		Prompt:     "go",
		WorkflowID: st.defaultWorkflow,
	})
	require.NoError(t, err)

	return created.ID
}
