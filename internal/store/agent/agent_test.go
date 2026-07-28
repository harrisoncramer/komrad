package agent

import (
	"context"
	"fmt"
	"testing"

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

// TestUpsert covers what an agent carries in and out of the store, including the
// config row it owns. An update has to rewrite that row rather than leave the
// old one orphaned behind a new id.
func TestUpsert(t *testing.T) {
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

			require.NoError(t, st.Upsert(ctx, tt.initial))
			stored, err := st.Get(ctx, tt.initial.Name)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.NotEmpty(t, stored.ID, "an id is generated on insert")
			assert.NotEmpty(t, stored.AgentConfigID, "an agent owns its config row")

			if tt.update != nil {
				updated := *stored
				tt.update(&updated)
				require.NoError(t, st.Upsert(ctx, updated))

				stored, err = st.Get(ctx, tt.initial.Name)
				require.NoError(t, err)
				require.NotNil(t, stored)
				assert.Equal(t, updated.AgentConfigID, stored.AgentConfigID, "the config id is stable across updates")
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

			require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
			first, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, first)

			if tt.archive {
				require.NoError(t, st.Archive(ctx, "agent"))
			}
			if tt.reuse {
				require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", Pane: "%2", State: StateNew}))
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

			changed, err := st.SetState(ctx, "agent", StateWaiting)
			require.NoError(t, err)
			require.True(t, changed)

			live, err = st.List(ctx)
			require.NoError(t, err)
			require.Len(t, live, 1)
			assert.Equal(t, StateWaiting, live[0].State, "a state event keyed by name lands on the live row")

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

// TestSetState guards the rule that a state write never creates a row. A write
// for an agent the tool did not create finds nothing and changes nothing, so an
// outside Claude session never leaks into the store.
func TestSetState(t *testing.T) {
	tests := []struct {
		name        string
		create      bool
		target      string
		wantChanged bool
		wantRows    int
	}{
		{
			name:   "an agent the tool never created",
			target: "external",
		},
		{
			name:     "a name that exists only as an archived row",
			create:   true,
			target:   "archived",
			wantRows: 1,
		},
		{
			name:        "a live agent",
			create:      true,
			target:      "agent",
			wantChanged: true,
			wantRows:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			if tt.create {
				require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", State: StateNew}))
			}

			changed, err := st.SetState(ctx, tt.target, StateThinking)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantRows, "a state write never inserts")
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
				require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", State: StateNew}))
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

// TestSetStateByIDKeepsTheMark pins that a state write is only a state write. The
// mark belongs to the user and the reconciler writes state constantly, so the two
// share a row without touching each other.
func TestSetStateByIDKeepsTheMark(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", State: StateNew}))
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
				require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", State: StateNew}))
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
	}{
		{
			name: "a restored agent keeps its id and its conversation",
		},
		{
			name:         "and the workflow it belonged to",
			withWorkflow: true,
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

			require.NoError(t, st.Upsert(ctx, agent))
			original, err := st.Get(ctx, "beta")
			require.NoError(t, err)
			require.NotNil(t, original)
			require.NoError(t, st.Archive(ctx, "beta"))

			require.NoError(t, st.Restore(ctx, original.ID))
			restored := *original
			restored.State = StateNew
			restored.Pane = "%9"
			require.NoError(t, st.Upsert(ctx, restored), "upsert on restore must not collide")

			got, err := st.Get(ctx, "beta")
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, original.ID, got.ID, "a restored row keeps its id")
			assert.Equal(t, StateNew, got.State)
			assert.Equal(t, "sess-1", got.ClaudeSessionID, "restore preserves the conversation")

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

			require.NoError(t, st.Upsert(ctx, Agent{Name: "alpha", Pane: "%1", State: StateNew}))

			if tt.hard {
				target, err := st.Find(ctx, "alpha")
				require.NoError(t, err)
				require.NotNil(t, target)
				require.NoError(t, st.Delete(ctx, *target))
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

			require.NoError(t, st.Upsert(ctx, Agent{Name: "archived", Pane: "%1", State: StateNew}))
			require.NoError(t, st.Archive(ctx, "archived"))
			require.NoError(t, st.Upsert(ctx, Agent{Name: "live", Pane: "%2", State: StateNew}))

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

// TestFindByID covers resolving the ids a hard delete needs from an id rather
// than a name. The trash deletes through this, and the rows there are exactly
// the ones a name cannot tell apart: the unique index holds among live rows
// only, so two archived agents may answer to the same name.
func TestFindByID(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Upsert(ctx, Agent{Name: "shared", Pane: "%1", State: StateNew}))
	first, err := st.Get(ctx, "shared")
	require.NoError(t, err)
	require.NotNil(t, first)

	require.NoError(t, st.Archive(ctx, "shared"))
	require.NoError(t, st.Upsert(ctx, Agent{Name: "shared", Pane: "%2", State: StateNew}))

	target, err := st.FindByID(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, first.ID, target.ID, "the id picks the archived agent out from the live one holding its name")
	assert.Equal(t, first.AgentConfigID, target.AgentConfigID, "the config to remove with it comes back too")

	missing, err := st.FindByID(ctx, "019fa434-e7a7-7ac9-bab5-718681da797d")
	require.NoError(t, err)
	assert.Nil(t, missing, "an id nobody holds is nil rather than an error")
}

// TestMoveToWorkflow covers filing an agent under a different workflow. There is
// nowhere to move an agent to but a workflow, so this replaces one with another
// rather than being able to clear it.
func TestMoveToWorkflow(t *testing.T) {
	tests := []struct {
		name string
		// move says whether the agent is reassigned at all, so a case can show what
		// an untouched agent still reports.
		move bool
	}{
		{
			name: "an agent stays in the workflow it was created in",
		},
		{
			name: "and moves when it is reassigned",
			move: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			elsewhere, err := st.workflows.Create(ctx, workflowstore.Config{Name: "elsewhere"})
			require.NoError(t, err)

			require.NoError(t, st.Upsert(ctx, Agent{Name: "scoped", State: StateNew}))
			require.NoError(t, st.Upsert(ctx, Agent{Name: "untouched", State: StateNew}))

			scoped, err := st.Get(ctx, "scoped")
			require.NoError(t, err)
			require.NotNil(t, scoped)
			assert.Equal(t, st.defaultWorkflow, scoped.WorkflowID)

			want := st.defaultWorkflow
			if tt.move {
				require.NoError(t, st.MoveToWorkflow(ctx, scoped.ID, elsewhere.ID))
				want = elsewhere.ID
			}

			got, err := st.Get(ctx, "scoped")
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, want, got.WorkflowID)

			other, err := st.Get(ctx, "untouched")
			require.NoError(t, err)
			require.NotNil(t, other)
			assert.Equal(t, st.defaultWorkflow, other.WorkflowID, "moving one agent leaves the others alone")

			all, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, all, 2, "the root list shows every agent regardless of workflow")
		})
	}
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

			require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", State: StateNew, IdempotencyKey: &tt.key}))
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

			require.NoError(t, st.Upsert(ctx, Agent{Name: "first", State: StateNew, IdempotencyKey: tt.first}))

			err := st.Upsert(ctx, Agent{Name: "second", State: StateNew, IdempotencyKey: tt.second})
			if tt.wantFail {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestCountLiveForSchedule covers the count that stops a schedule lapping
// itself. Anything unfinished counts, including an agent waiting on a
// confirmation, since that is work still owed rather than work done.
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
			name:   "and one that has not started yet",
			states: []string{StateNew},
			want:   1,
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
				require.NoError(t, st.Upsert(ctx, Agent{Name: name, State: state, ScheduleID: &schedule}))
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

// Upsert fills in the default workflow when the test did not name one.
func (f *fixture) Upsert(ctx context.Context, agent Agent) error {
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.defaultWorkflow
	}

	return f.Store.Upsert(ctx, agent)
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
