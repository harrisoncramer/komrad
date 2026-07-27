package schedule

import (
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	workflowstore "claude-agents/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// missingID is a well-formed uuid no schedule holds. A malformed one would be
// refused before the lookup and prove something else.
const missingID = "00000000-0000-0000-0000-000000000000"

func TestGet(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		archive  bool
		id       string
		wantName string
	}{
		{
			name: "an id nobody holds",
			id:   missingID,
		},
		{
			name: "a malformed id is not a schedule",
			id:   "not-an-id",
		},
		{
			name:    "an archived schedule is hidden",
			create:  true,
			archive: true,
		},
		{
			name:     "a schedule that exists",
			create:   true,
			wantName: "morning-ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			id := tt.id
			if tt.create {
				created := seed(t, st, Config{Name: "morning-ci", Cron: "0 9 * * 1-5", Prompt: "check CI", Enabled: true})
				id = created.ID
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, id))
			}

			got, err := st.Get(ctx, id)
			require.NoError(t, err)

			if tt.wantName == "" {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, "0 9 * * 1-5", got.Cron)
			assert.Equal(t, "check CI", got.Prompt)
			assert.True(t, got.Enabled)
		})
	}
}

// TestCreate covers what a new schedule carries, including the config row it
// owns and the fields a caller may leave out.
func TestCreate(t *testing.T) {
	tests := []struct {
		name         string
		cfg          Config
		wantCwd      string
		wantFlags    string
		wantEnabled  bool
		wantWorkflow bool
	}{
		{
			name: "the fields a schedule was given are read back",
			cfg: Config{
				Name: "morning-ci", Cron: "0 9 * * 1-5", Timezone: "America/New_York",
				Enabled: true, Prompt: "check CI", Cwd: "/work/api", Flags: "--model opus",
			},
			wantCwd:     "/work/api",
			wantFlags:   "--model opus",
			wantEnabled: true,
		},
		{
			name:        "a paused schedule is stored paused",
			cfg:         Config{Name: "draft", Cron: "0 9 * * *", Prompt: "later"},
			wantEnabled: false,
		},
		{
			name: "a schedule filed under a workflow keeps the link",
			cfg: Config{
				Name: "scoped", Cron: "0 9 * * *", Prompt: "go", Enabled: true,
			},
			wantEnabled:  true,
			wantWorkflow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			cfg := tt.cfg
			var workflowID string
			if tt.wantWorkflow {
				workflowID = seedWorkflow(t, st)
				cfg.WorkflowID = &workflowID
			}

			created, err := st.Create(ctx, cfg)
			require.NoError(t, err)
			require.NotNil(t, created)
			assert.NotEmpty(t, created.ID, "an id is generated on insert")

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, cfg.Name, got.Name)
			assert.Equal(t, cfg.Cron, got.Cron)
			assert.Equal(t, cfg.Timezone, got.Timezone)
			assert.Equal(t, tt.wantEnabled, got.Enabled)
			assert.Equal(t, tt.wantCwd, got.Cwd)
			assert.Equal(t, tt.wantFlags, got.Flags)

			if !tt.wantWorkflow {
				assert.Nil(t, got.WorkflowID)
				return
			}
			require.NotNil(t, got.WorkflowID)
			assert.Equal(t, workflowID, *got.WorkflowID)
		})
	}
}

// TestNameTaken covers the unique index on live names. It is scoped to live
// rows, so an archived schedule holds its name without blocking a new one.
func TestNameTaken(t *testing.T) {
	tests := []struct {
		name     string
		archive  bool
		wantTake bool
	}{
		{
			name:     "a live schedule holds its name",
			wantTake: true,
		},
		{
			name:    "an archived one releases it",
			archive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			first := seed(t, st, Config{Name: "morning-ci", Cron: "0 9 * * *", Prompt: "go"})
			if tt.archive {
				require.NoError(t, st.Archive(ctx, first.ID))
			}

			_, err := st.Create(ctx, Config{Name: "morning-ci", Cron: "0 9 * * *", Prompt: "again"})
			if tt.wantTake {
				require.ErrorIs(t, err, ErrNameTaken)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestUpdate covers replacing a schedule's fields, including the config row it
// owns and the workflow link, which is the one field that can be cleared.
func TestUpdate(t *testing.T) {
	tests := []struct {
		name         string
		startScoped  bool
		endScoped    bool
		update       Config
		wantName     string
		wantEnabled  bool
		wantCwd      string
		wantWorkflow bool
	}{
		{
			name:        "the editable fields are replaced",
			update:      Config{Name: "renamed", Cron: "0 17 * * 5", Timezone: "UTC", Prompt: "weekly", Cwd: "/work/web"},
			wantName:    "renamed",
			wantEnabled: false,
			wantCwd:     "/work/web",
		},
		{
			name:        "a paused schedule can be resumed through an update",
			update:      Config{Name: "morning-ci", Cron: "0 9 * * *", Enabled: true, Prompt: "go", Cwd: "/work/api"},
			wantName:    "morning-ci",
			wantEnabled: true,
			wantCwd:     "/work/api",
		},
		{
			name:         "a workflow can be attached",
			endScoped:    true,
			update:       Config{Name: "morning-ci", Cron: "0 9 * * *", Prompt: "go"},
			wantName:     "morning-ci",
			wantWorkflow: true,
		},
		{
			name:        "and cleared again",
			startScoped: true,
			update:      Config{Name: "morning-ci", Cron: "0 9 * * *", Prompt: "go"},
			wantName:    "morning-ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			initial := Config{Name: "morning-ci", Cron: "0 9 * * 1-5", Prompt: "check CI", Cwd: "/work/api"}
			if tt.startScoped {
				id := seedWorkflow(t, st)
				initial.WorkflowID = &id
			}
			created := seed(t, st, initial)

			update := tt.update
			var workflowID string
			if tt.endScoped {
				workflowID = seedWorkflow(t, st)
				update.WorkflowID = &workflowID
			}

			require.NoError(t, st.Update(ctx, created.ID, update))

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, update.Cron, got.Cron)
			assert.Equal(t, tt.wantEnabled, got.Enabled)
			assert.Equal(t, tt.wantCwd, got.Cwd)

			if !tt.wantWorkflow {
				assert.Nil(t, got.WorkflowID, "an omitted workflow clears the link")
				return
			}
			require.NotNil(t, got.WorkflowID)
			assert.Equal(t, workflowID, *got.WorkflowID)
		})
	}
}

// TestSetEnabled covers the one-keystroke pause, which must leave every other
// field alone.
func TestSetEnabled(t *testing.T) {
	tests := []struct {
		name    string
		start   bool
		set     bool
		id      string
		wantEnd bool
	}{
		{
			name:    "pausing a live schedule",
			start:   true,
			set:     false,
			wantEnd: false,
		},
		{
			name:    "resuming a paused one",
			start:   false,
			set:     true,
			wantEnd: true,
		},
		{
			name:    "an id nobody holds changes nothing",
			start:   true,
			set:     false,
			id:      missingID,
			wantEnd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created := seed(t, st, Config{Name: "morning-ci", Cron: "0 9 * * *", Prompt: "check CI", Enabled: tt.start})

			target := tt.id
			if target == "" {
				target = created.ID
			}
			require.NoError(t, st.SetEnabled(ctx, target, tt.set))

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantEnd, got.Enabled)
			assert.Equal(t, "check CI", got.Prompt, "pausing leaves the rest of the record alone")
		})
	}
}

// TestList covers what the listing shows, which is live schedules only.
func TestList(t *testing.T) {
	tests := []struct {
		name      string
		seeds     []string
		archive   string
		wantNames []string
	}{
		{
			name:      "an empty store lists nothing",
			wantNames: []string{},
		},
		{
			name:      "live schedules are listed",
			seeds:     []string{"a", "b"},
			wantNames: []string{"b", "a"},
		},
		{
			name:      "an archived one drops out",
			seeds:     []string{"a", "b"},
			archive:   "b",
			wantNames: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			byName := make(map[string]string, len(tt.seeds))
			for _, name := range tt.seeds {
				created := seed(t, st, Config{Name: name, Cron: "0 9 * * *", Prompt: "go"})
				byName[name] = created.ID
			}
			if tt.archive != "" {
				require.NoError(t, st.Archive(ctx, byName[tt.archive]))
			}

			listed, err := st.List(ctx)
			require.NoError(t, err)

			names := make([]string, 0, len(listed))
			for _, s := range listed {
				names = append(names, s.Name)
			}
			assert.ElementsMatch(t, tt.wantNames, names)
		})
	}
}

// TestListPage walks the listing through its cursor the way the RPC does.
func TestListPage(t *testing.T) {
	tests := []struct {
		name      string
		seeds     int
		limit     int64
		wantPages int
	}{
		{
			name:      "one page holds everything when the limit is large",
			seeds:     3,
			limit:     10,
			wantPages: 1,
		},
		{
			name:      "a small limit pages through the whole set",
			seeds:     3,
			limit:     1,
			wantPages: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			for i := range tt.seeds {
				seed(t, st, Config{Name: string(rune('a' + i)), Cron: "0 9 * * *", Prompt: "go"})
			}

			var (
				seen   []string
				cursor *Cursor
				pages  int
			)
			for range tt.seeds + 1 {
				page, err := st.ListPage(ctx, tt.limit, cursor)
				require.NoError(t, err)
				if len(page) == 0 {
					break
				}

				pages++
				for _, s := range page {
					seen = append(seen, s.ID)
				}

				last := page[len(page)-1]
				cursor = &Cursor{UpdatedAt: last.UpdatedAt, ID: last.ID}
			}

			assert.Equal(t, tt.wantPages, pages)
			assert.Len(t, seen, tt.seeds, "every schedule is returned exactly once")
		})
	}
}

// fixture is the schedule store under test plus the workflow store the tests seed
// through, since a schedule may point at a workflow and that row has to exist
// first. They stay separate stores, because this package never writes that table
// itself.
type fixture struct {
	*Store
	workflows *workflowstore.Store
}

// open builds the stores over a throwaway database. The tests run against real
// Postgres because what this package does is SQL, the partial unique index on live
// names, the keyset page, and the join to the owned config row.
func open(t *testing.T) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &fixture{Store: New(pool.DB()), workflows: workflowstore.New(pool.DB())}
}

// seed creates a schedule and returns it, since the tests need the generated id
// rather than the fields they just wrote.
func seed(t *testing.T, st *fixture, cfg Config) *Schedule {
	t.Helper()

	created, err := st.Create(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, created)

	return created
}

// seedWorkflow creates a workflow and returns its id, for the cases about a
// schedule that files its agents under one.
func seedWorkflow(t *testing.T, st *fixture) string {
	t.Helper()

	created, err := st.workflows.Create(t.Context(), workflowstore.Config{Name: "backend"})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}
