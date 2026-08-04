package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"weave/db/models"
	"weave/internal/pgtest"
	"weave/internal/store"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

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
		{
			name: "a newline becomes a dash",
			in:   "ab\ncd",
			want: "ab-cd",
		},
		{
			name: "a delete becomes a dash",
			in:   "ab\x7fcd",
			want: "ab-cd",
		},
		{
			name: "characters above the ascii control range are kept",
			in:   "caf\u00e9\u00a0",
			want: "caf\u00e9\u00a0",
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
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}
			if tt.reuse {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%2", State: StateIdle}))
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
			assert.Equal(t, StateIdle, got.State)
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

// TestArchiveClearsPane guards the pane id on an archived row, which the reconciler
// never clears because it skips archived agents.
func TestArchiveClearsPane(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	require.NoError(t, st.archiveByName(ctx, "agent"))

	archived, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, archived)
	assert.Empty(t, archived.Pane, "an archived agent claims no pane")
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
			wantState: StateIdle,
		},
		{
			name:      "an id that is not an id",
			target:    "not-a-uuid",
			wantState: StateIdle,
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

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
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

// TestSetMarks covers marking, changing a mark, clearing one, and who may carry one,
// over one agent and over several at once. A write that silently landed on nothing
// would leave the caller believing they had marked agents that show no mark.
func TestSetMarks(t *testing.T) {
	tests := []struct {
		name    string
		create  int
		archive bool
		ids     func(created []Agent) []string
		// before is put on every created agent ahead of the write, so a row can start
		// from a mark rather than from nothing.
		before     *string
		mark       *string
		wantMarked int
	}{
		{
			name: "an id no agent holds",
			ids:  func([]Agent) []string { return []string{uuid.NewString()} },
			mark: xptr.Ptr("●"),
		},
		{
			name: "a value that is not an id at all",
			ids:  func([]Agent) []string { return []string{"not-an-id"} },
			mark: xptr.Ptr("●"),
		},
		{
			name:    "an archived agent, which has no listing to be marked in",
			create:  1,
			archive: true,
			ids:     func(created []Agent) []string { return []string{created[0].ID} },
			mark:    xptr.Ptr("●"),
		},
		{
			name:       "a value that is not an id, which must not take the agent beside it down",
			create:     1,
			ids:        func(created []Agent) []string { return []string{"not-an-id", created[0].ID} },
			mark:       xptr.Ptr("●"),
			wantMarked: 1,
		},
		{
			name:       "an id nobody holds beside one somebody does",
			create:     1,
			ids:        func(created []Agent) []string { return []string{created[0].ID, uuid.NewString()} },
			mark:       xptr.Ptr("●"),
			wantMarked: 1,
		},
		{
			name:       "marking a live agent",
			create:     1,
			ids:        agentIDs,
			mark:       xptr.Ptr("●"),
			wantMarked: 1,
		},
		{
			name:       "marking several at once",
			create:     3,
			ids:        agentIDs,
			mark:       xptr.Ptr("●"),
			wantMarked: 3,
		},
		{
			name:       "moving several agents from one mark to another",
			create:     2,
			ids:        agentIDs,
			before:     xptr.Ptr("●"),
			mark:       xptr.Ptr("✓"),
			wantMarked: 2,
		},
		{
			name:       "clearing the marks several agents carry",
			create:     2,
			ids:        agentIDs,
			before:     xptr.Ptr("●"),
			wantMarked: 2,
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
				marked, err := st.SetMarks(ctx, agentIDs(created), tt.before)
				require.NoError(t, err)
				require.Equal(t, len(created), marked)
			}
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, created[0].Name))
			}

			marked, err := st.SetMarks(ctx, tt.ids(created), tt.mark)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMarked, marked)

			carrying := 0
			for _, agent := range created {
				after, err := st.GetByID(ctx, agent.ID)
				require.NoError(t, err)
				require.NotNil(t, after)

				if assert.ObjectsAreEqual(tt.mark, after.Mark) {
					carrying++
				}
			}

			assert.Equal(t, tt.wantMarked, carrying)
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
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
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
				require.NoError(t, st.archiveByName(ctx, "agent"))
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

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
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

// TestSetMetadataCeilingUnderConcurrency drives more writers at one agent's key limit than fit, testing locking.
func TestSetMetadataCeilingUnderConcurrency(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	const (
		room    = 4
		writers = 16
	)

	for i := range maxMetadataKeys - room {
		changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: fmt.Sprintf("key-%d", i), Value: xptr.Ptr("v")})
		require.NoError(t, err)
		require.True(t, changed)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)

	for i := range writers {

		wg.Go(func() {

			_, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: fmt.Sprintf("racer-%d", i), Value: xptr.Ptr("v")})
			errs[i] = err
		})
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, ErrTooManyMetadataKeys, "writer %d", i)
		}
	}

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Len(t, after.Metadata, maxMetadataKeys, "only room many racers should have gotten past the limit")
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
		require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))
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

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	changed, err := st.SetMetadata(ctx, SetMetadataParams{ID: created.ID, Key: "pr", Value: xptr.Ptr("https://github.com/o/r/pull/1")})
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, st.Delete(ctx, []string{created.ID}))

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

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	marked, err := st.SetMarks(ctx, []string{created.ID}, xptr.Ptr("●"))
	require.NoError(t, err)
	require.Equal(t, 1, marked)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateWaiting))

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, StateWaiting, after.State)
	assert.Equal(t, "●", *after.Mark, "a state write leaves the user's mark standing")
}

// TestSetStateByIDStampsOnlyOnAChange covers what state_changed_at means. The
// reconciler writes the state it observed every pass, so a stamp that moved on every
// write would say when the agent was last looked at rather than when it entered the
// state it holds.
func TestSetStateByIDStampsOnlyOnAChange(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	moved, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, moved)
	assert.True(t, moved.StateChangedAt.After(created.StateChangedAt), "entering a new state stamps it")

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	again, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Equal(t, moved.StateChangedAt, again.StateChangedAt, "observing the same state again leaves it")
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

// TestActiveNameExists covers who owns a name. The unique index is scoped to live rows in
// one workflow, so an archived agent keeps its name without blocking a new one, and so
// does a live agent in another workflow.
func TestActiveNameExists(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		// elsewhere files the agent under a second workflow, where the name it holds is
		// none of the first workflow's business.
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
				require.NoError(t, st.Archive(ctx, []string{archived.ID}))
			}

			exists, err := st.ActiveNameExists(ctx, st.defaultWorkflow, "agent")
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

			agent := Agent{Name: "beta", State: StateThinking, SessionID: "sess-1"}
			scope := st.defaultWorkflow
			if tt.withWorkflow {
				workflow, err := workflows.Create(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				agent.WorkflowID = workflow.ID
				scope = workflow.ID
			}

			require.NoError(t, st.Create(ctx, agent))
			original, err := st.GetInWorkflow(ctx, scope, "beta")
			require.NoError(t, err)
			require.NotNil(t, original)
			if tt.hidden {
				changed, err := st.SetHidden(ctx, original.ID, true)
				require.NoError(t, err)
				require.True(t, changed)
			}
			require.NoError(t, st.Archive(ctx, []string{original.ID}))

			require.NoError(t, st.Restore(ctx, []string{original.ID}))
			restored := *original
			restored.State = StateIdle
			restored.Pane = "%9"
			require.NoError(t, st.Update(ctx, restored), "an update on restore must not collide")

			got, err := st.GetInWorkflow(ctx, scope, "beta")
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, original.ID, got.ID, "a restored row keeps its id")
			assert.Equal(t, StateIdle, got.State)
			assert.Equal(t, "sess-1", got.SessionID, "restore preserves the conversation")
			assert.False(t, got.Hidden, "a restored agent comes back to a row")
			assert.Nil(t, got.ArchivedAt, "and carries no deletion time")

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

			require.NoError(t, st.Create(ctx, Agent{Name: "alpha", Pane: "%1", State: StateIdle}))

			if tt.hard {
				created, err := st.Get(ctx, "alpha")
				require.NoError(t, err)
				require.NotNil(t, created)

				require.NoError(t, st.Delete(ctx, []string{created.ID}))
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

// TestListPageArchived covers the two sets a listing can ask for. They are what
// the panel and its archive each show, and an agent in one must never appear in
// the other: the tree would offer keys on a row that is gone, and the archive
// would offer to destroy one that is running.
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

// TestDeleteByIDPicksTheArchivedRow covers why a hard delete keys on the id. The
// archive deletes through it, and the rows there are exactly the ones a name
// cannot tell apart: the unique index holds among live rows only, so an archived
// agent can share a name with a live one.
func TestDeleteByIDPicksTheArchivedRow(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "shared", Pane: "%1", State: StateIdle}))
	first, err := st.Get(ctx, "shared")
	require.NoError(t, err)
	require.NotNil(t, first)

	require.NoError(t, st.archiveByName(ctx, "shared"))
	require.NoError(t, st.Create(ctx, Agent{Name: "shared", Pane: "%2", State: StateIdle}))

	require.NoError(t, st.Delete(ctx, []string{first.ID}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "only the archived row was destroyed")
	assert.NotEqual(t, first.ID, all[0].ID, "the live agent holding the same name is untouched")
	assert.Equal(t, "%2", all[0].Pane)
}

// TestGetByID covers the lookup every call that keys on an id makes. It has to span
// the archived agents: an id is the only handle an archived agent still answers to,
// since its name may have been taken by a live one since. The ids that are not ids
// matter because a restore or a delete asked for through the MCP tools arrives as
// whatever string the caller typed.
func TestGetByID(t *testing.T) {
	tests := []struct {
		name string
		// lookFor is the id to look the agent up under, given the agent that was
		// created, so a case can ask for that agent or for something that is not it.
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
			require.NoError(t, err)

			if !tt.wantHit {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, created.ID, got.ID)
			assert.Equal(t, "agent", got.Name)
		})
	}
}

// TestGetByIDCarriesMetadata covers the one thing the listing this replaced did not.
// A restore hands the agent back to whoever asked for it, and what that agent recorded
// about its own work is part of the record.
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

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle, IdempotencyKey: &tt.key}))
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
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

			require.NoError(t, st.Create(ctx, Agent{Name: "first", State: StateIdle, IdempotencyKey: tt.first}))

			err := st.Create(ctx, Agent{Name: "second", State: StateIdle, IdempotencyKey: tt.second})
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
// work still owed. An idle one does not, and that includes one never seen working,
// since nothing will ever move it and the schedule would be blocked for good.
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
			// Idle covers both the agent that finished and the launch that never got
			// going, since only an agent observed working is ever moved off it.
			name:   "an idle one does not",
			states: []string{StateIdle},
			want:   0,
		},
		{
			name:   "several unfinished agents are all counted",
			states: []string{StateThinking, StateWaiting, StateIdle},
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
					require.NoError(t, st.archiveByName(ctx, name))
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

// TestCountLive covers the count a create is measured against, which every row the panel
// holds contributes to whatever its state and whether or not it is shown.
func TestCountLive(t *testing.T) {
	tests := []struct {
		name string
		// states are the agents to write, one per entry.
		states  []string
		hide    bool
		archive bool
		want    int64
	}{
		{
			name: "a panel holding nothing has room",
		},
		{
			name:   "an agent still thinking counts",
			states: []string{StateThinking},
			want:   1,
		},
		{
			// Idle is both the agent that finished and the dormant one whose pane is gone.
			name:   "so does an idle one, which is still a row to be dealt with",
			states: []string{StateIdle},
			want:   1,
		},
		{
			name:   "every state at once",
			states: []string{StateThinking, StateWaiting, StateIdle},
			want:   3,
		},
		{
			name:   "a hidden agent counts, since hiding it stopped nothing",
			states: []string{StateThinking},
			hide:   true,
			want:   1,
		},
		{
			name:    "an archived agent does not, which is how the count comes down",
			states:  []string{StateThinking, StateIdle},
			archive: true,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			for i, state := range tt.states {
				name := fmt.Sprintf("agent-%d", i)
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: state}))

				written, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, written)

				if tt.hide {
					hidden, err := st.SetHidden(ctx, written.ID, true)
					require.NoError(t, err)
					require.True(t, hidden)
				}
				if tt.archive {
					require.NoError(t, st.Archive(ctx, []string{written.ID}))
				}
			}

			count, err := st.CountLive(ctx)
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

// Get reads an agent back by name out of the default workflow, which is the scope a
// name is unique in.
// agentIDs is the ids of a set of agents, which is what the calls keyed on a list take.
func agentIDs(agents []Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}

	return ids
}

func (f *fixture) Get(ctx context.Context, name string) (*Agent, error) {
	return f.GetInWorkflow(ctx, f.defaultWorkflow, name)
}

// archiveByName archives the agent the default workflow holds under a name, since these
// tests seed by name and Archive is keyed by id.
func (f *fixture) archiveByName(ctx context.Context, name string) error {
	agent, err := f.GetInWorkflow(ctx, f.defaultWorkflow, name)
	if err != nil {
		return err
	}
	if agent == nil {
		return fmt.Errorf("no live agent called %q", name)
	}

	return f.Archive(ctx, []string{agent.ID})
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

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{Store: New(p.GetDB()), db: p.GetDB(), workflows: workflows, defaultWorkflow: created.ID}
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

// TestListByID covers the read a batch does before it writes. It is the one lookup that
// has to say which of the ids it was given found nothing, so what it leaves out matters as
// much as what it returns.
func TestListByID(t *testing.T) {
	tests := []struct {
		name string
		// create is the agents seeded, by name, before the lookup.
		create []string
		// ids picks what to look up out of the seeded agents and whatever strays the case
		// adds of its own.
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

// TestListByIDCarriesMetadata pins the metadata coming back with a batch read, the way it
// does for one agent and for a page. A restore hands its agents straight to the caller, so
// what this leaves off is what the panel draws.
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

// TestArchiveAndRestoreInBatches covers the two writes a selection drives. Both are keyed
// on a list of ids, so what they must not do is act on a row nobody named.
func TestArchiveAndRestoreInBatches(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	other, err := st.workflows.Create(ctx, workflowstore.Config{Name: "frontend"})
	require.NoError(t, err)

	names := map[string]string{"auth": st.defaultWorkflow, "api": st.defaultWorkflow, "ui": other.ID}
	ids := make(map[string]string, len(names))
	for name, workflowID := range names {
		require.NoError(t, st.Create(ctx, Agent{Name: name, Pane: "%1", State: StateIdle, WorkflowID: workflowID}))

		created, err := st.GetInWorkflow(ctx, workflowID, name)
		require.NoError(t, err)
		require.NotNil(t, created)
		ids[name] = created.ID
	}

	require.NoError(t, st.Archive(ctx, []string{ids["auth"], ids["ui"]}))

	live, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1, "the agent nobody named is untouched")
	assert.Equal(t, "api", live[0].Name)

	for _, name := range []string{"auth", "ui"} {
		archived, err := st.GetByID(ctx, ids[name])
		require.NoError(t, err)
		require.NotNil(t, archived)
		assert.NotNil(t, archived.ArchivedAt, "%s is archived", name)
		assert.Empty(t, archived.Pane, "and claims no pane")
	}

	require.NoError(t, st.Restore(ctx, []string{ids["auth"], ids["ui"]}))

	live, err = st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 3, "the batch came back whole, across both workflows")
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

	require.NoError(t, st.Delete(ctx, []string{ids[0], "not-an-id", ids[2]}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the id that could hold no row took nothing with it")
	assert.Equal(t, "api", all[0].Name)
}
