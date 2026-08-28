package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

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

// TestSpawnedByAgentID covers the parent an agent records when another agent created it.
// One a person started through the panel carries no caller header at all, so it has to
// read back nil rather than some sentinel standing in for nobody.
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

// TestDeleteNullsSpawnedByAgentID covers what happens to a child when its parent is
// removed for good. Losing a parent must not take the work its children did with it, so
// the reference is nulled rather than the child row going with it.
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

// TestArchiveAndReuse covers who a name belongs to over an agent's life. An
// archived row keeps its name, so reusing it has to create a fresh row rather
// than resurrect the dead one, and state events keyed by name must land on the
// live row.
func TestArchiveAndReuse(t *testing.T) {
	tests := []struct {
		name    string
		archive bool

		reuse bool

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

		target string

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

// TestSetTags covers tagging, changing a tag, clearing one, and who may carry one,
// over one agent and over several at once. A write that silently landed on nothing
// would leave the caller believing they had tagged agents that show no tag.
func TestSetTags(t *testing.T) {
	tests := []struct {
		name    string
		create  int
		archive bool
		ids     func(created []Agent) []string

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

// TestSetHidden covers hiding an agent, bringing it back, and who may be hidden at
// all. A write that landed on nothing would leave the panel drawing a row the caller
// believes it took away.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		id      func(created Agent) string

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
			name:        "an archived agent, which is how an archive records what it was for",
			create:      true,
			archive:     true,
			id:          func(created Agent) string { return created.ID },
			key:         "pr",
			value:       xptr.Ptr("https://github.com/o/r/pull/1"),
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
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

	_, deleteErr := st.Delete(ctx, []string{created.ID})

	require.NoError(t, deleteErr)

	orphaned, err := models.New(st.db).ListAgentMetadata(ctx, []string{created.ID})
	require.NoError(t, err)
	assert.Empty(t, orphaned)
}

// TestSetStateByIDKeepsTheTag pins that a state write is only a state write. The
// tag belongs to the user and the reconciler writes state constantly, so the two
// share a row without touching each other.
func TestSetStateByIDKeepsTheTag(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	tagged, err := st.SetTags(ctx, []string{created.ID}, xptr.Ptr("●"))
	require.NoError(t, err)
	require.Equal(t, 1, tagged)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateWaiting))

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, StateWaiting, after.State)
	assert.Equal(t, "●", *after.Tag, "a state write leaves the user's tag standing")
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

// TestActiveNameExists covers who owns a name. The unique index is scoped to live rows in
// one workflow, so an archived agent keeps its name without blocking a new one, and so
// does a live agent in another workflow.
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

// TestRestore mirrors the manager's restore ordering at the store layer:
// undelete by id, then upsert the same id. It must not collide on the primary
// key, and what the agent belonged to has to survive the round trip.
func TestRestore(t *testing.T) {
	tests := []struct {
		name string

		withWorkflow bool

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
			_, archiveErr := st.Archive(ctx, []string{original.ID})
			require.NoError(t, archiveErr)

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

	_, deleteErr := st.Delete(ctx, []string{first.ID})

	require.NoError(t, deleteErr)

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

		states []string

		archive bool

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
					_, archiveErr := st.Archive(ctx, []string{written.ID})
					require.NoError(t, archiveErr)
				}
			}

			count, err := st.CountLive(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, count)
		})
	}
}

// TestANudgeIsHeldForTheRetryInterval covers what the interval buys. A nudge just taken
// stands, and the same nudge left standing longer than the interval is there to be taken
// again, which is the whole of what a caller asks for by passing one.
func TestANudgeIsHeldForTheRetryInterval(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "idler", Pane: "%1", State: StateIdle}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)

	claimed, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed, "an agent never nudged is there to be nudged")

	again, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	assert.False(t, again, "and not again while the one just taken still stands")

	backdateNudge(t, st, agent.ID, 2*time.Minute)

	again, err = st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	assert.True(t, again, "a nudge older than the interval is spent")
}

// TestANudgeIsReclaimedOnTheDatabaseClock covers which clock the interval is counted on.
// nudged_at is stamped by the database, so what it is compared against has to come from the
// database too. Counted from the caller's clock the two agree only to within whatever the
// clocks drift by, and inside a transaction they do not agree at all: NOW() stays at the
// transaction's start while the caller's clock runs on, so a nudge taken moments ago reads
// as one left standing for however long the transaction has been open.
func TestANudgeIsReclaimedOnTheDatabaseClock(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "idler", Pane: "%1", State: StateIdle}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)

	claimed, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, st.pool.InTx(ctx, func(db store.DBTX) error {
		time.Sleep(300 * time.Millisecond)

		again, err := New(db).ClaimNudge(ctx, agent.ID, 200*time.Millisecond)
		require.NoError(t, err)
		assert.False(t, again, "no database time has passed, so the nudge still stands")

		return nil
	}))
}

func backdateNudge(t *testing.T, st *fixture, id string, age time.Duration) {
	t.Helper()

	_, err := st.db.Exec(t.Context(),
		`UPDATE agents SET nudged_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

type fixture struct {
	*Store
	db        store.DBTX
	pool      *store.Pool
	workflows *workflowstore.Store

	defaultWorkflow string
}

func agentIDs(agents []Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}

	return ids
}

// Get reads an agent back by name out of the default workflow, which is the scope a
// name is unique in.
func (f *fixture) Get(ctx context.Context, name string) (*Agent, error) {
	return f.GetInWorkflow(ctx, f.defaultWorkflow, name)
}

func (f *fixture) archiveByName(ctx context.Context, name string) error {
	agent, err := f.GetInWorkflow(ctx, f.defaultWorkflow, name)
	if err != nil {
		return err
	}
	if agent == nil {
		return fmt.Errorf("no live agent called %q", name)
	}

	_, err = f.Archive(ctx, []string{agent.ID})

	return err
}

// Create fills in the default workflow when the test did not name one.
func (f *fixture) Create(ctx context.Context, agent Agent) error {
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.defaultWorkflow
	}

	return f.Store.Create(ctx, agent)
}

func open(t *testing.T) *fixture {
	t.Helper()
	p := pool(t)

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{Store: New(p.GetDB()), db: p.GetDB(), pool: p, workflows: workflows, defaultWorkflow: created.ID}
}

func pool(t *testing.T) *store.Pool {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return p
}

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

// TestArchiveAndDeleteReturnThePaneTheRowHeld covers what an archive and a delete report back
// about the rows they wrote. An archive blanks the pane in the same statement, so what it has
// to hand back is the pane as the row held it rather than the blank it just wrote: killing the
// CLI is the caller's next step, and by then there is nowhere else the pane is written down.
func TestArchiveAndDeleteReturnThePaneTheRowHeld(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	seedPane := func(name, pane string) string {
		t.Helper()
		require.NoError(t, st.Create(ctx, Agent{Name: name, Pane: pane, State: StateIdle}))

		created, err := st.GetInWorkflow(ctx, st.defaultWorkflow, name)
		require.NoError(t, err)
		require.NotNil(t, created)

		return created.ID
	}

	archiving := seedPane("archiving", "%4")
	deleting := seedPane("deleting", "%5")

	archived, err := st.Archive(ctx, []string{archiving})
	require.NoError(t, err)
	assert.Equal(t, []AgentPane{{AgentID: archiving, Pane: "%4"}}, archived)

	deleted, err := st.Delete(ctx, []string{deleting})
	require.NoError(t, err)
	assert.Equal(t, []AgentPane{{AgentID: deleting, Pane: "%5"}}, deleted)
}

// TestArchiveReturnsThePaneAWriteItWaitedOnLeftBehind covers the archive against the write it
// actually races: a resume or a create recording its pane in its own statement, uncommitted,
// while the archive is already blocked on the row. Reading the pane from a plain join against
// the table hands back the value from before that write, since only the row being updated is
// re-read once the lock is released. The archive would then commit, blank the pane, and report
// nothing to kill, which is the state it exists to prevent.
func TestArchiveReturnsThePaneAWriteItWaitedOnLeftBehind(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "dormant", State: StateIdle}))

	created, err := st.GetInWorkflow(ctx, st.defaultWorkflow, "dormant")
	require.NoError(t, err)
	require.NotNil(t, created)

	tx, err := st.pool.GetRawPool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `UPDATE agents SET pane_id = '%9' WHERE id = $1`, created.ID)
	require.NoError(t, err)

	type outcome struct {
		taken []AgentPane
		err   error
	}

	archived := make(chan outcome, 1)
	go func() {
		taken, archiveErr := st.Archive(context.WithoutCancel(ctx), []string{created.ID})
		archived <- outcome{taken: taken, err: archiveErr}
	}()

	waitForBlockedArchive(t, st)
	require.NoError(t, tx.Commit(ctx))

	got := <-archived
	require.NoError(t, got.err)
	assert.Equal(t, []AgentPane{{AgentID: created.ID, Pane: "%9"}}, got.taken)
}

// waitForBlockedArchive blocks until an archive is waiting on a row lock, which is how the test
// knows the archive has reached its write rather than not having started yet.
func waitForBlockedArchive(t *testing.T, st *fixture) {
	t.Helper()

	for range 500 {
		var waiting bool
		require.NoError(t, st.pool.GetRawPool().QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT
					1
				FROM
					pg_stat_activity
				WHERE
					wait_event_type = 'Lock'
					AND query LIKE '%MarkAgentsArchived%')`).Scan(&waiting))

		if waiting {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("no archive ever blocked on the row, so it never reached its write")
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

	_, archiveErr := st.Archive(ctx, []string{ids["auth"], ids["ui"]})

	require.NoError(t, archiveErr)

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

	_, deleteErr := st.Delete(ctx, []string{ids[0], "not-an-id", ids[2]})

	require.NoError(t, deleteErr)

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the id that could hold no row took nothing with it")
	assert.Equal(t, "api", all[0].Name)
}
