package agent

import (
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	workflowstore "claude-agents/internal/store/workflow"

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
			st, workflows := openWithWorkflows(t)
			ctx := t.Context()

			agent := Agent{Name: "beta", State: StateThinking, ClaudeSessionID: "sess-1"}
			if tt.withWorkflow {
				workflow, err := workflows.Create(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				agent.WorkflowID = &workflow.ID
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

			if !tt.withWorkflow {
				assert.Nil(t, got.WorkflowID)
				return
			}
			require.NotNil(t, got.WorkflowID, "restore preserves the workflow")
			assert.Equal(t, *original.WorkflowID, *got.WorkflowID)
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

// TestMoveToWorkflow covers filing an agent under a workflow and taking it back
// out. The root listing is unscoped either way, so a move changes what the agent
// belongs to without changing what the panel shows at the top level.
func TestMoveToWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		assign       bool
		thenUnassign bool
		wantAssigned bool
	}{
		{
			name: "a new agent belongs to no workflow",
		},
		{
			name:         "assigning files it under the workflow",
			assign:       true,
			wantAssigned: true,
		},
		{
			name:         "a nil target takes it back out",
			assign:       true,
			thenUnassign: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, workflows := openWithWorkflows(t)
			ctx := t.Context()

			workflow, err := workflows.Create(ctx, workflowstore.Config{Name: "backend"})
			require.NoError(t, err)
			require.NoError(t, st.Upsert(ctx, Agent{Name: "scoped", State: StateNew}))
			require.NoError(t, st.Upsert(ctx, Agent{Name: "unscoped", State: StateNew}))

			scoped, err := st.Get(ctx, "scoped")
			require.NoError(t, err)
			require.NotNil(t, scoped)
			assert.Nil(t, scoped.WorkflowID, "a new agent is unassigned")

			if tt.assign {
				require.NoError(t, st.MoveToWorkflow(ctx, scoped.ID, &workflow.ID))
			}
			if tt.thenUnassign {
				require.NoError(t, st.MoveToWorkflow(ctx, scoped.ID, nil))
			}

			got, err := st.Get(ctx, "scoped")
			require.NoError(t, err)
			require.NotNil(t, got)
			if tt.wantAssigned {
				require.NotNil(t, got.WorkflowID)
				assert.Equal(t, workflow.ID, *got.WorkflowID)
			} else {
				assert.Nil(t, got.WorkflowID)
			}

			all, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, all, 2, "the root list shows every agent regardless of workflow")
		})
	}
}

// open builds an agent store over a throwaway database. The tests run against
// real Postgres because most of what this package does is SQL: the partial
// unique index on live names, the keyset page, the generated ids.
func open(t *testing.T) *Store {
	t.Helper()

	return New(pool(t).DB())
}

// openWithWorkflows builds the agent store alongside a workflow store over the
// same database, for the tests that need a workflow to exist before an agent can
// point at it. The two are separate stores even here: this package never reaches
// into the workflows table itself.
func openWithWorkflows(t *testing.T) (*Store, *workflowstore.Store) {
	t.Helper()
	p := pool(t)

	return New(p.DB()), workflowstore.New(p.DB())
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
