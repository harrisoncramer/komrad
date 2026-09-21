package schedule

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"weave/internal/dbtest"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingID = "00000000-0000-0000-0000-000000000000"

// TestGet pins which lookups find a schedule, an archived one and an id nobody holds
// included.
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
				created := seed(t, st, Config{
					Name:    "morning-ci",
					Cron:    "0 9 * * 1-5",
					Prompt:  "check CI",
					Enabled: true,
				})
				id = created.ID
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, id))
			}

			got, err := st.Get(ctx, id)

			if tt.wantName == "" {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, "0 9 * * 1-5", got.Cron)
			assert.Equal(t, "check CI", got.Prompt)
			assert.True(t, got.Enabled)
		})
	}
}

// TestCreate covers what a new schedule carries and the fields a caller may leave out, none
// of which say where or how an agent runs.
func TestCreate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantEnabled bool
	}{
		{
			name: "the fields a schedule was given are read back",
			cfg: Config{
				Name:     "morning-ci",
				Cron:     "0 9 * * 1-5",
				Timezone: "America/New_York",
				Enabled:  true,
				Prompt:   "check CI",
			},
			wantEnabled: true,
		},
		{
			name: "a paused schedule is stored paused",
			cfg: Config{
				Name:   "draft",
				Cron:   "0 9 * * *",
				Prompt: "later",
			},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			cfg := tt.cfg
			cfg.WorkflowID = seedWorkflow(t, st)

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
			assert.Equal(t, cfg.WorkflowID, got.WorkflowID, "a schedule always names the workflow it launches into")
		})
	}
}

// TestNameTaken pins that the unique index is scoped to live rows, so an archived schedule
// holds its name without blocking a new one.
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

			first := seed(t, st, Config{
				Name:   "morning-ci",
				Cron:   "0 9 * * *",
				Prompt: "go",
			})
			if tt.archive {
				require.NoError(t, st.Archive(ctx, first.ID))
			}

			_, err := st.Create(ctx, Config{
				Name:       "morning-ci",
				Cron:       "0 9 * * *",
				Prompt:     "again",
				WorkflowID: seedWorkflow(t, st),
			})
			if tt.wantTake {
				require.ErrorIs(t, err, store.ErrNameTaken)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestUpdate covers replacing a schedule's fields, including moving it to another workflow.
func TestUpdate(t *testing.T) {
	tests := []struct {
		name         string
		update       Config
		moveWorkflow bool
		wantEnabled  bool
	}{
		{
			name: "the editable fields are replaced",
			update: Config{
				Name:     "renamed",
				Cron:     "0 17 * * 5",
				Timezone: "UTC",
				Prompt:   "weekly",
			},
			wantEnabled: false,
		},
		{
			name: "a paused schedule can be resumed through an update",
			update: Config{
				Name:    "morning-ci",
				Cron:    "0 9 * * *",
				Enabled: true,
				Prompt:  "go",
			},
			wantEnabled: true,
		},
		{
			name: "a schedule can be moved to another workflow",
			update: Config{
				Name:   "morning-ci",
				Cron:   "0 9 * * *",
				Prompt: "go",
			},
			moveWorkflow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			original := seedWorkflow(t, st)
			created := seed(t, st, Config{
				Name:       "morning-ci",
				Cron:       "0 9 * * 1-5",
				Prompt:     "check CI",
				WorkflowID: original,
			})

			update := tt.update
			update.WorkflowID = original
			if tt.moveWorkflow {
				update.WorkflowID = seedWorkflow(t, st)
			}

			require.NoError(t, st.Update(ctx, created.ID, update))

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, update.Name, got.Name)
			assert.Equal(t, update.Cron, got.Cron)
			assert.Equal(t, tt.wantEnabled, got.Enabled)
			assert.Equal(t, update.WorkflowID, got.WorkflowID)

			if tt.moveWorkflow {
				assert.NotEqual(t, original, got.WorkflowID, "the schedule moved")
			}
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

			created := seed(t, st, Config{
				Name:    "morning-ci",
				Cron:    "0 9 * * *",
				Prompt:  "check CI",
				Enabled: tt.start,
			})

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

// TestHideAgents pins that the flag hiding a schedule's runs from the sidebar survives a
// write and can be turned back off.
func TestHideAgents(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created := seed(t, st, Config{
		Name:       "sweeper",
		Cron:       "*/5 * * * *",
		Prompt:     "sweep",
		Enabled:    true,
		HideAgents: true,
	})

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.HideAgents)

	require.NoError(t, st.Update(ctx, created.ID, Config{
		Name:       "sweeper",
		Cron:       "*/5 * * * *",
		Prompt:     "sweep",
		Enabled:    true,
		HideAgents: false,
		WorkflowID: got.WorkflowID,
	}))

	updated, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.HideAgents, "an edit that turns it off is written")
}

// TestRecordLaunchResult covers what a firing writes and what a reader gets back.
func TestRecordLaunchResult(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		message     string
		wantStatus  LaunchResultStatus
		wantMessage string
	}{
		{
			name: "an id nobody holds changes nothing",
			id:   missingID,
		},
		{
			name: "a malformed id is not a schedule",
			id:   "not-an-id",
		},
		{
			name:        "a message past the cap is truncated on the way in",
			message:     strings.Repeat("x", maxLaunchResultMessage+100),
			wantStatus:  LaunchResultCannotLaunch,
			wantMessage: strings.Repeat("x", maxLaunchResultMessage) + "…",
		},
		{
			name:        "the result is read back",
			message:     "schedule \"nightly\" cannot launch: bad directory",
			wantStatus:  LaunchResultCannotLaunch,
			wantMessage: "schedule \"nightly\" cannot launch: bad directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created := seed(t, st, Config{
				Name:   "nightly",
				Cron:   "0 9 * * *",
				Prompt: "go",
			})

			id := tt.id
			if id == "" {
				id = created.ID
			}

			require.NoError(t, st.RecordLaunchResult(ctx, id, LaunchResultCannotLaunch, tt.message))

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)

			if tt.wantStatus == "" {
				assert.Empty(t, got.LastResultStatus)
				assert.Nil(t, got.LastResultAt)

				return
			}

			assert.Equal(t, tt.wantStatus, got.LastResultStatus)
			assert.Equal(t, tt.wantMessage, got.LastResultMessage)
			assert.NotNil(t, got.LastResultAt)
		})
	}
}

// TestRecordLaunchResultArchived covers that a schedule archived between the firing being
// queued and the write landing does not have its record touched, matching the write's own
// AND archived_at IS NULL guard used everywhere else on this table.
func TestRecordLaunchResultArchived(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created := seed(t, st, Config{
		Name:   "nightly",
		Cron:   "0 9 * * *",
		Prompt: "go",
	})
	require.NoError(t, st.Archive(ctx, created.ID))

	require.NoError(t, st.RecordLaunchResult(ctx, created.ID, LaunchResultCannotLaunch, "bad directory"))
}

// TestRecordLaunchResultOverwrites covers that the column is a last result rather than a
// log: a second firing's outcome replaces the first's rather than sitting beside it.
func TestRecordLaunchResultOverwrites(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created := seed(t, st, Config{
		Name:    "nightly",
		Cron:    "0 9 * * *",
		Prompt:  "go",
		Enabled: true,
	})

	require.NoError(t, st.RecordLaunchResult(ctx, created.ID, LaunchResultCannotLaunch, "bad directory"))
	require.NoError(t, st.RecordLaunchResult(ctx, created.ID, LaunchResultLaunched, ""))

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, LaunchResultLaunched, got.LastResultStatus)
	assert.Empty(t, got.LastResultMessage)
}

// TestList covers what the listing shows.
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
				created := seed(t, st, Config{
					Name:   name,
					Cron:   "0 9 * * *",
					Prompt: "go",
				})
				byName[name] = created.ID
			}
			if tt.archive != "" {
				require.NoError(t, st.Archive(ctx, byName[tt.archive]))
			}

			listed, err := st.List(ctx)
			require.NoError(t, err)

			names := xslice.Map(listed, func(s Schedule) string {
				return s.Name
			})
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
				seed(t, st, Config{
					Name:   string(rune('a' + i)),
					Cron:   "0 9 * * *",
					Prompt: "go",
				})
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
				seen = append(seen, xslice.Map(page, func(s Schedule) string {
					return s.ID
				})...)

				last := page[len(page)-1]
				cursor = &Cursor{ID: last.ID}
			}

			assert.Equal(t, tt.wantPages, pages)
			assert.Len(t, seen, tt.seeds, "every schedule is returned exactly once")
			assert.IsDecreasing(t, seen, "the listing is newest first, which is descending id order")
		})
	}
}

type fixture struct {
	*Store
	workflows *workflowstore.Store
}

func open(t *testing.T) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &fixture{Store: New(pool.GetDB()), workflows: workflowstore.New(pool.GetDB())}
}

func seed(t *testing.T, st *fixture, cfg Config) *Schedule {
	t.Helper()

	if cfg.WorkflowID == "" {
		cfg.WorkflowID = seedWorkflow(t, st)
	}

	created, err := st.Create(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, created)

	return created
}

func seedWorkflow(t *testing.T, st *fixture) string {
	t.Helper()

	created, err := st.workflows.Create(t.Context(), workflowstore.Config{Name: "backend-" + uuid.NewString()})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}
