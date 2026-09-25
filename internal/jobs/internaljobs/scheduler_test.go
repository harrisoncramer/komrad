package internaljobs

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"komrad/internal/schedules"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xslice"

	"github.com/riverqueue/river"
	robfig "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var neverOccurring = []string{"0 0 30 2 *", "0 0 31 2 *", "0 0 31 4 *"}

type scheduleRow struct {
	key      string
	name     string
	cron     string
	timezone string
	paused   bool
}

type scheduleWriter struct {
	stores     *storetest.Stores
	workflowID string
	ids        map[string]string
	keys       map[string]string
}

type recordingPeriodic struct {
	added   []string
	removed []string
	refused string
}

// TestSchedulerReconcile pins which schedules a pass registers and which it takes off.
func TestSchedulerReconcile(t *testing.T) {
	tests := []struct {
		name        string
		passes      [][]scheduleRow
		failFor     string
		wantErr     bool
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name: "a registration River refuses leaves the rest registered",
			passes: [][]scheduleRow{{
				newArmedRow("older", "0 9 * * *"),
				newArmedRow("newer", "0 10 * * *"),
			}},
			failFor:   "newer",
			wantErr:   true,
			wantAdded: []string{"newer", "older"},
		},
		{
			name: "and a schedule paused since the last pass still comes off",
			passes: [][]scheduleRow{
				{newArmedRow("good", "0 9 * * *")},
				{
					newArmedRow("broken", "0 10 * * *"),
					newPausedRow("good", "0 9 * * *"),
				},
			},
			failFor:     "broken",
			wantErr:     true,
			wantAdded:   []string{"good", "broken"},
			wantRemoved: []string{"good"},
		},
		{
			name: "an unparseable expression is skipped, leaving the rest registered",
			passes: [][]scheduleRow{{
				newArmedRow("bad", "0 99 * * *"),
				newArmedRow("good", "0 9 * * *"),
			}},
			wantAdded: []string{"good"},
		},
		{
			name:   "a paused schedule is not registered",
			passes: [][]scheduleRow{{newPausedRow("s1", "0 9 * * *")}},
		},
		{
			name: "a schedule paused since the last pass is taken off",
			passes: [][]scheduleRow{
				{newArmedRow("s1", "0 9 * * *")},
				{newPausedRow("s1", "0 9 * * *")},
			},
			wantAdded:   []string{"s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name: "so is one archived since the last pass",
			passes: [][]scheduleRow{
				{newArmedRow("s1", "0 9 * * *")},
				{},
			},
			wantAdded:   []string{"s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name: "a second pass over an unchanged schedule registers nothing again",
			passes: [][]scheduleRow{
				{newArmedRow("s1", "0 9 * * *")},
				{newArmedRow("s1", "0 9 * * *")},
			},
			wantAdded: []string{"s1"},
		},
		{
			name: "renaming a schedule leaves its registration alone",
			passes: [][]scheduleRow{
				{newArmedRow("s1", "0 9 * * *")},
				{renameRow(newArmedRow("s1", "0 9 * * *"), "new-name")},
			},
			wantAdded: []string{"s1"},
		},
		{
			name: "a changed expression replaces the registration rather than adding a second",
			passes: [][]scheduleRow{
				{newArmedRow("s1", "0 9 * * *")},
				{newArmedRow("s1", "*/5 * * * *")},
			},
			wantAdded:   []string{"s1", "s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name: "a changed timezone counts as a change too",
			passes: [][]scheduleRow{
				{newArmedRow("s1", "0 9 * * *")},
				{setRowZone(newArmedRow("s1", "0 9 * * *"), "America/New_York")},
			},
			wantAdded:   []string{"s1", "s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name:      "an armed schedule is registered",
			passes:    [][]scheduleRow{{newArmedRow("s1", "0 9 * * *")}},
			wantAdded: []string{"s1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			writer := newScheduleWriter(t, stores)
			periodic := &recordingPeriodic{}
			scheduler := NewScheduler(stores.Schedules, periodic)

			var errs []error
			for _, pass := range tt.passes {
				writer.apply(t, pass)
				periodic.refused = writer.ids[tt.failFor]
				errs = append(errs, scheduler.Reconcile(t.Context()))
			}

			assert.Equal(t, tt.wantErr, errors.Join(errs...) != nil)
			assert.Equal(t, tt.wantAdded, writer.lookupKeys(periodic.added))
			assert.Equal(t, tt.wantRemoved, writer.lookupKeys(periodic.removed))
		})
	}
}

// TestSchedulerReconcileFailure pins that a listing the reconciler could not read disarms nothing.
func TestSchedulerReconcileFailure(t *testing.T) {
	stores := storetest.Open(t)
	writer := newScheduleWriter(t, stores)
	periodic := &recordingPeriodic{}
	scheduler := NewScheduler(stores.Schedules, periodic)

	writer.apply(t, []scheduleRow{newArmedRow("s1", "0 9 * * *")})
	require.NoError(t, scheduler.Reconcile(t.Context()))

	require.NoError(t, stores.Pool.Close())

	require.Error(t, scheduler.Reconcile(t.Context()))
	assert.Empty(t, periodic.removed, "a failed pass must not read as every schedule having been archived")
}

// TestOccurrenceFor covers which firing a job is filed under, whenever River's enqueuer gets to
// building it.
func TestOccurrenceFor(t *testing.T) {
	minute := time.Date(2026, time.July, 28, 9, 34, 0, 0, time.UTC)
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		cron string
		at   time.Time
		want time.Time
	}{
		{
			name: "a firing built a hair early belongs to the occurrence it beat",
			cron: "* * * * *",
			at:   minute.Add(-2 * time.Millisecond),
			want: minute,
		},
		{
			name: "one built most of a minute late still belongs to that minute",
			cron: "* * * * *",
			at:   minute.Add(45 * time.Second),
			want: minute,
		},
		{
			name: "one built a minute late on an hourly schedule belongs to the hour",
			cron: "0 * * * *",
			at:   nine.Add(90 * time.Second),
			want: nine,
		},
		{
			name: "and half an hour late on a daily one belongs to the morning",
			cron: "0 9 * * *",
			at:   nine.Add(30 * time.Minute),
			want: nine,
		},
		{
			name: "a firing built on the boundary belongs to itself",
			cron: "* * * * *",
			at:   minute,
			want: minute,
		},
		{
			name: "and one a hair after it, which is the ordinary case",
			cron: "* * * * *",
			at:   minute.Add(7 * time.Millisecond),
			want: minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cron, err := schedules.ParseCron(tt.cron, "UTC")
			require.NoError(t, err)

			assert.Equal(t, tt.want, occurrenceFor(cron, tt.at).UTC())
		})
	}
}

// TestOccurrenceForNeverOccurs covers a schedule with no occurrence, where every step comes
// back as the zero time and without a stop the walk spins inside River's enqueuer.
func TestOccurrenceForNeverOccurs(t *testing.T) {
	for _, expression := range neverOccurring {
		t.Run(expression, func(t *testing.T) {
			schedule, err := robfig.ParseStandard(expression)
			require.NoError(t, err, "the expression parses, which is what puts it here")

			returned := make(chan time.Time, 1)
			go func() { returned <- occurrenceFor(schedule, time.Now()) }()

			select {
			case got := <-returned:
				assert.True(t, got.IsZero(), "a schedule with no occurrence names none")
			case <-time.After(3 * time.Second):
				t.Fatal("occurrenceFor did not return within 3s, so it is spinning")
			}
		})
	}
}

// TestLaunchArgsForNeverOccurs covers such a schedule queueing nothing, nil arguments being how
// River is told there is no job to insert.
func TestLaunchArgsForNeverOccurs(t *testing.T) {
	for _, expression := range neverOccurring {
		t.Run(expression, func(t *testing.T) {
			schedule, err := robfig.ParseStandard(expression)
			require.NoError(t, err)

			args, _ := LaunchArgsFor("s1", schedule, time.Now())
			assert.Nil(t, args)
		})
	}
}

// TestLaunchArgsForOccurrence covers the ordinary case, where the arguments name the
// schedule and the firing they stand for.
func TestLaunchArgsForOccurrence(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	cron, err := schedules.ParseCron("0 9 * * *", "UTC")
	require.NoError(t, err)

	args, _ := LaunchArgsFor("s1", cron, nine.Add(30*time.Minute))
	launch, ok := args.(LaunchAgentArgs)
	require.True(t, ok)

	assert.Equal(t, "s1", launch.ScheduleID)
	assert.Equal(t, nine, launch.ScheduledFor.UTC())
}

func newArmedRow(key, cron string) scheduleRow {
	return scheduleRow{
		key:  key,
		cron: cron,
	}
}

func newPausedRow(key, cron string) scheduleRow {
	row := newArmedRow(key, cron)
	row.paused = true

	return row
}

func renameRow(row scheduleRow, name string) scheduleRow {
	row.name = name

	return row
}

func setRowZone(row scheduleRow, timezone string) scheduleRow {
	row.timezone = timezone

	return row
}

func newScheduleWriter(t *testing.T, stores *storetest.Stores) *scheduleWriter {
	t.Helper()

	return &scheduleWriter{
		stores:     stores,
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{Name: "nightly"}).ID,
		ids:        map[string]string{},
		keys:       map[string]string{},
	}
}

func (w *scheduleWriter) apply(t *testing.T, pass []scheduleRow) {
	t.Helper()

	kept := map[string]bool{}
	for _, row := range pass {
		kept[row.key] = true

		name := row.name
		if name == "" {
			name = row.key
		}
		cfg := schedulestore.Config{
			Name:       name,
			Cron:       row.cron,
			Timezone:   row.timezone,
			Enabled:    !row.paused,
			Prompt:     "go",
			WorkflowID: w.workflowID,
		}

		if id, ok := w.ids[row.key]; ok {
			require.NoError(t, w.stores.Schedules.Update(t.Context(), id, cfg))

			continue
		}

		created, err := w.stores.Schedules.Create(t.Context(), cfg)
		require.NoError(t, err)
		w.ids[row.key] = created.ID
		w.keys[created.ID] = row.key
	}

	for key, id := range w.ids {
		if !kept[key] {
			require.NoError(t, w.stores.Schedules.Archive(t.Context(), id))
		}
	}
}

func (w *scheduleWriter) lookupKeys(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}

	return xslice.Map(ids, func(id string) string {
		return w.keys[id]
	})
}

func (r *recordingPeriodic) Add(id string, cron river.PeriodicSchedule) error {
	if cron == nil {
		return errors.New("a registration with nothing to fire on")
	}

	r.added = append(r.added, id)

	if id == r.refused {
		return fmt.Errorf("refusing to register %q", id)
	}

	return nil
}

func (r *recordingPeriodic) Remove(id string) {
	r.removed = append(r.removed, id)
}
