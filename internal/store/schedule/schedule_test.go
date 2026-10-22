package schedule

import (
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/store/task/tasktest"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var missingID = uuid.MustParse("00000000-0000-0000-0000-000000000000")

// TestGet pins which lookups find a schedule, an archived one and an id nobody holds
// included.
func TestGet(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		archive  bool
		id       uuid.UUID
		wantName string
	}{
		{
			name: "an id nobody holds",
			id:   missingID,
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

// TestKind pins that a schedule's kind and the orchestrator it mails are read back, and that the
// two are only ever written together.
func TestKind(t *testing.T) {
	tests := []struct {
		name              string
		kind              Kind
		namesOrchestrator bool
		namesStranger     bool
		wantErr           bool
		wantKind          Kind
	}{
		{
			name:     "a schedule given no kind starts a task",
			wantKind: KindTask,
		},
		{
			name:              "an orchestrator schedule names the orchestrator it mails",
			kind:              KindOrchestrator,
			namesOrchestrator: true,
			wantKind:          KindOrchestrator,
		},
		{
			name:    "an orchestrator schedule naming no orchestrator is refused",
			kind:    KindOrchestrator,
			wantErr: true,
		},
		{
			name:          "an orchestrator schedule naming an agent komrad does not hold is refused",
			kind:          KindOrchestrator,
			namesStranger: true,
			wantErr:       true,
		},
		{
			name:              "a task schedule naming an orchestrator is refused",
			kind:              KindTask,
			namesOrchestrator: true,
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			cfg := Config{
				Name:       "nightly",
				Cron:       "0 9 * * *",
				Prompt:     "go",
				WorkflowID: seedWorkflow(t, st),
				Kind:       tt.kind,
			}
			if tt.namesOrchestrator {
				orchestrator := seedAgent(t, st, cfg.WorkflowID)
				cfg.OrchestratorAgentID = &orchestrator
			}
			if tt.namesStranger {
				stranger := missingID
				cfg.OrchestratorAgentID = &stranger
			}

			created, err := st.Create(ctx, cfg)
			if tt.wantErr {
				require.Error(t, err)

				existing := seed(t, st, Config{
					Name:   "existing",
					Cron:   "0 9 * * *",
					Prompt: "go",
				})
				cfg.Name = existing.Name
				cfg.WorkflowID = existing.WorkflowID
				require.Error(t, st.Update(ctx, existing.ID, cfg), "nor is one rewritten that way")

				return
			}
			require.NoError(t, err)

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, got.Kind)
			assert.Equal(t, cfg.OrchestratorAgentID, got.OrchestratorAgentID)
		})
	}
}

// TestListWatched covers the watched set read whole and by key.
func TestListWatched(t *testing.T) {
	tests := []struct {
		name      string
		seeds     []string
		archive   string
		keys      []string
		wantNames []string
	}{
		{
			name:      "an empty store lists nothing",
			wantNames: []string{},
		},
		{
			name:      "nil keys list every schedule",
			seeds:     []string{"a", "b"},
			wantNames: []string{"b", "a"},
		},
		{
			name:      "an archived one stays in the whole set",
			seeds:     []string{"a", "b"},
			archive:   "b",
			wantNames: []string{"a", "b"},
		},
		{
			name:      "keys list only the schedules they name",
			seeds:     []string{"a", "b"},
			keys:      []string{"a"},
			wantNames: []string{"a"},
		},
		{
			name:      "an archived key is listed",
			seeds:     []string{"a", "b"},
			archive:   "b",
			keys:      []string{"b"},
			wantNames: []string{"b"},
		},
		{
			name:      "keys naming no schedule list nothing",
			seeds:     []string{"a"},
			keys:      []string{"nobody"},
			wantNames: []string{},
		},
		{
			name:      "an empty key list lists nothing",
			seeds:     []string{"a"},
			keys:      []string{},
			wantNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			byName := make(map[string]uuid.UUID, len(tt.seeds))
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

			listed, err := st.ListWatched(ctx, resolveSeedNames(tt.keys, byName))
			require.NoError(t, err)

			names := xslice.Map(listed, func(s Schedule) string {
				return s.Name
			})
			assert.ElementsMatch(t, tt.wantNames, names)
			for _, schedule := range listed {
				assert.Equal(t, schedule.Name == tt.archive, schedule.ArchivedAt != nil, "%s says whether it is archived", schedule.Name)
			}
		})
	}
}

// TestListPage walks the listing through its cursor the way the RPC does.
func TestListPage(t *testing.T) {
	tests := []struct {
		name         string
		seeds        int
		archived     int
		listArchived bool
		limit        int64
		wantPages    int
		wantSeen     int
	}{
		{
			name:      "an archived schedule leaves the listing",
			seeds:     3,
			archived:  1,
			limit:     10,
			wantPages: 1,
			wantSeen:  2,
		},
		{
			name:         "the archived listing holds only the archived ones",
			seeds:        3,
			archived:     2,
			listArchived: true,
			limit:        1,
			wantPages:    2,
			wantSeen:     2,
		},
		{
			name:      "one page holds everything when the limit is large",
			seeds:     3,
			limit:     10,
			wantPages: 1,
			wantSeen:  3,
		},
		{
			name:      "a small limit pages through the whole set",
			seeds:     3,
			limit:     1,
			wantPages: 3,
			wantSeen:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			for i := range tt.seeds {
				created := seed(t, st, Config{
					Name:   string(rune('a' + i)),
					Cron:   "0 9 * * *",
					Prompt: "go",
				})
				if i < tt.archived {
					require.NoError(t, st.Archive(ctx, created.ID))
				}
			}

			var (
				seen   []uuid.UUID
				cursor *Cursor
				pages  int
			)
			for range tt.seeds + 1 {
				page, err := st.ListPage(ctx, tt.limit, cursor, tt.listArchived)
				require.NoError(t, err)
				if len(page) == 0 {
					break
				}

				pages++
				seen = append(seen, xslice.Map(page, func(s Schedule) uuid.UUID {
					return s.ID
				})...)

				last := page[len(page)-1]
				cursor = &Cursor{ID: last.ID}
			}

			assert.Equal(t, tt.wantPages, pages)
			assert.Len(t, seen, tt.wantSeen, "every schedule on the listed side is returned exactly once")
			assert.IsDecreasing(t, seen, "the listing is newest first, which is descending id order")
		})
	}
}

type fixture struct {
	*ScheduleStore
	db        store.DBTX
	workflows *workflowstore.WorkflowStore
	agents    *agentstore.AgentStore
}

// TestRestore pins which schedules come back out of the archive and when a live one's name
// stands in the way.
func TestRestore(t *testing.T) {
	tests := []struct {
		name       string
		archive    bool
		nameTaken  bool
		wantErr    error
		wantLive   bool
		wantHidden bool
	}{
		{
			name:     "an archived schedule comes back",
			archive:  true,
			wantLive: true,
		},
		{
			name:     "a live schedule is left as it is",
			wantLive: true,
		},
		{
			name:       "a live schedule holding its name refuses it",
			archive:    true,
			nameTaken:  true,
			wantErr:    store.ErrNameTaken,
			wantHidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created := seed(t, st, Config{
				Name:    "morning-ci",
				Cron:    "0 9 * * *",
				Prompt:  "go",
				Enabled: true,
			})
			if tt.archive {
				require.NoError(t, st.Archive(ctx, created.ID))
			}
			if tt.nameTaken {
				seed(t, st, Config{
					Name:   "morning-ci",
					Cron:   "0 9 * * *",
					Prompt: "again",
				})
			}

			err := st.Restore(ctx, created.ID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			got, err := st.GetIncludingArchived(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHidden, got.ArchivedAt != nil)
			assert.True(t, got.Enabled, "restoring leaves whether it is armed alone")
		})
	}
}

// TestDelete pins that deleting a schedule takes its firings with it and leaves the agents it
// launched in place with no schedule.
func TestDelete(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created := seed(t, st, Config{
		Name:   "morning-ci",
		Cron:   "0 9 * * *",
		Prompt: "go",
	})
	require.NoError(t, st.StartRun(ctx, StartRunParams{
		Occurrence: RunOccurrence{
			ScheduleID:   created.ID,
			ScheduledFor: time.Now(),
		},
		Attempt:     1,
		MaxAttempts: 3,
	}))
	taskID, err := tasktest.Seed(ctx, st.db, created.WorkflowID)
	require.NoError(t, err)
	agent := agentstore.Agent{
		Name:       "launched",
		WorkflowID: created.WorkflowID,
		ScheduleID: &created.ID,
		State:      agentstore.StateIdle,
		TaskID:     taskID,
	}
	require.NoError(t, st.agents.Create(ctx, agent))

	require.NoError(t, st.Delete(ctx, created.ID))

	_, err = st.GetIncludingArchived(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	runs, err := st.ListRuns(ctx, created.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, runs, "its firings go with it")

	launched, err := st.agents.GetInWorkflow(ctx, created.WorkflowID, "launched")
	require.NoError(t, err)
	assert.Nil(t, launched.ScheduleID, "the agent it launched stays and no longer names it")
}

// TestDeletingAnOrchestratorTakesItsSchedules leaves a schedule mailing another orchestrator alone.
func TestDeletingAnOrchestratorTakesItsSchedules(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	workflow := seedWorkflow(t, st)
	gone := seedAgent(t, st, workflow)
	kept := seedAgent(t, st, workflow)

	mailingGone := seed(t, st, Config{
		Name:                "gone",
		Cron:                "0 9 * * *",
		Prompt:              "go",
		WorkflowID:          workflow,
		Kind:                KindOrchestrator,
		OrchestratorAgentID: &gone,
	})
	mailingKept := seed(t, st, Config{
		Name:                "kept",
		Cron:                "0 9 * * *",
		Prompt:              "go",
		WorkflowID:          workflow,
		Kind:                KindOrchestrator,
		OrchestratorAgentID: &kept,
	})

	_, err := st.agents.Delete(ctx, []uuid.UUID{gone})
	require.NoError(t, err)

	_, err = st.GetIncludingArchived(ctx, mailingGone.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.Get(ctx, mailingKept.ID)
	require.NoError(t, err)
}

func open(t *testing.T) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &fixture{
		ScheduleStore: NewScheduleStore(pool.GetDB()),
		db:            pool.GetDB(),
		workflows:     workflowstore.NewWorkflowStore(pool.GetDB()),
		agents:        agentstore.NewAgentStore(pool.GetDB()),
	}
}

func seed(t *testing.T, st *fixture, cfg Config) *Schedule {
	t.Helper()

	if cfg.WorkflowID == uuid.Nil {
		cfg.WorkflowID = seedWorkflow(t, st)
	}

	created, err := st.Create(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, created)

	return created
}

func seedWorkflow(t *testing.T, st *fixture) uuid.UUID {
	t.Helper()

	created, err := st.workflows.Create(t.Context(), workflowstore.Config{Name: "backend-" + uuid.NewString(), Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

func seedAgent(t *testing.T, st *fixture, workflowID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()
	taskID, err := tasktest.Seed(t.Context(), st.db, workflowID)
	require.NoError(t, err)
	require.NoError(t, st.agents.Create(t.Context(), agentstore.Agent{
		TaskID:     taskID,
		ID:         id,
		Name:       "orchestrator-" + id.String(),
		WorkflowID: workflowID,
		State:      agentstore.StateIdle,
	}))

	return id
}

func resolveSeedNames(keys []string, byName map[string]uuid.UUID) []uuid.UUID {
	if keys == nil {
		return nil
	}

	return xslice.Map(keys, func(key string) uuid.UUID {
		if id, ok := byName[key]; ok {
			return id
		}

		return uuid.New()
	})
}
