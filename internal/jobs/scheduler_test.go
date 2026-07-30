package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	schedulestore "claude-agents/internal/store/schedule"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchedulerReconcile covers which schedules a pass registers with River and
// which it takes back off. Registering is not idempotent from River's side, so a
// pass that re-adds a schedule nothing changed about would leave two jobs firing
// on the same expression.
func TestSchedulerReconcile(t *testing.T) {
	tests := []struct {
		name string
		// passes are the schedule listings the reconciler sees, one per pass, so a
		// case can show what a second pass does with what the first registered.
		passes [][]schedulestore.Schedule
		// wantAdded and wantRemoved are the schedule ids River was told about,
		// across every pass.
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name:      "an armed schedule is registered",
			passes:    [][]schedulestore.Schedule{{armed("s1", "0 9 * * *")}},
			wantAdded: []string{"s1"},
		},
		{
			name:   "a paused one is not",
			passes: [][]schedulestore.Schedule{{paused("s1", "0 9 * * *")}},
		},
		{
			// The API refuses an unparseable expression, so this is a row written
			// before a rule changed. One bad schedule must not stop the others.
			name: "an unparseable expression is skipped, leaving the rest registered",
			passes: [][]schedulestore.Schedule{{
				armed("bad", "0 99 * * *"),
				armed("good", "0 9 * * *"),
			}},
			wantAdded: []string{"good"},
		},
		{
			name: "a second pass over an unchanged schedule registers nothing again",
			passes: [][]schedulestore.Schedule{
				{armed("s1", "0 9 * * *")},
				{armed("s1", "0 9 * * *")},
			},
			wantAdded: []string{"s1"},
		},
		{
			// The name is not what the job fires on, so editing it must not churn
			// the registration.
			name: "renaming a schedule leaves its registration alone",
			passes: [][]schedulestore.Schedule{
				{armed("s1", "0 9 * * *")},
				{renamed(armed("s1", "0 9 * * *"), "new-name")},
			},
			wantAdded: []string{"s1"},
		},
		{
			name: "a changed expression replaces the registration rather than adding a second",
			passes: [][]schedulestore.Schedule{
				{armed("s1", "0 9 * * *")},
				{armed("s1", "*/5 * * * *")},
			},
			wantAdded:   []string{"s1", "s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name: "a changed timezone counts as a change too",
			passes: [][]schedulestore.Schedule{
				{armed("s1", "0 9 * * *")},
				{inZone(armed("s1", "0 9 * * *"), "America/New_York")},
			},
			wantAdded:   []string{"s1", "s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name: "a schedule paused since the last pass is taken off",
			passes: [][]schedulestore.Schedule{
				{armed("s1", "0 9 * * *")},
				{paused("s1", "0 9 * * *")},
			},
			wantAdded:   []string{"s1"},
			wantRemoved: []string{"s1"},
		},
		{
			name: "so is one deleted since the last pass",
			passes: [][]schedulestore.Schedule{
				{armed("s1", "0 9 * * *")},
				{},
			},
			wantAdded:   []string{"s1"},
			wantRemoved: []string{"s1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing := &listingSchedules{}
			periodic := &recordingPeriodic{}
			scheduler := NewScheduler(listing, periodic)

			for _, pass := range tt.passes {
				listing.schedules = pass
				require.NoError(t, scheduler.Reconcile(t.Context()))
			}

			assert.Equal(t, tt.wantAdded, periodic.added)
			assert.Equal(t, tt.wantRemoved, periodic.removed)
		})
	}
}

// TestSchedulerReconcileFailure covers a listing the reconciler could not read.
// It has to surface rather than be swallowed as an empty listing, which would
// read as every schedule having been deleted and take them all off River.
func TestSchedulerReconcileFailure(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantErr     bool
		wantRemoved int
	}{
		{
			name:    "a failed listing is an error",
			err:     errors.New("connection refused"),
			wantErr: true,
		},
		{
			name: "an empty listing is not",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing := &listingSchedules{err: tt.err}
			periodic := &recordingPeriodic{}
			scheduler := NewScheduler(listing, periodic)

			err := scheduler.Reconcile(t.Context())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Len(t, periodic.removed, tt.wantRemoved, "a failed pass must not disarm anything")
		})
	}
}

// TestSchedulerReconcileSurvivesAFailedRegistration covers a pass where River
// refuses one schedule. An unparseable expression is already skipped so that one
// bad row cannot stop the others, and a registration that fails is the same
// situation arriving from the other side: the rest of the listing still has to be
// registered, and a schedule paused or deleted since the last pass still has to
// come off, or it keeps firing.
func TestSchedulerReconcileSurvivesAFailedRegistration(t *testing.T) {
	tests := []struct {
		name string
		// failFor is the schedule id River refuses to register.
		failFor string
		// passes are the schedule listings the reconciler sees, one per pass.
		passes [][]schedulestore.Schedule
		// wantAdded is the ids registration was attempted for, across every pass.
		wantAdded []string
		// wantRemoved is the ids taken back off.
		wantRemoved []string
	}{
		{
			name:    "the schedules after the failure are still registered",
			failFor: "first",
			passes: [][]schedulestore.Schedule{{
				armed("first", "0 9 * * *"),
				armed("second", "0 10 * * *"),
			}},
			wantAdded: []string{"first", "second"},
		},
		{
			name:    "and a schedule paused since the last pass still comes off",
			failFor: "broken",
			passes: [][]schedulestore.Schedule{
				{armed("good", "0 9 * * *")},
				{
					armed("broken", "0 10 * * *"),
					paused("good", "0 9 * * *"),
				},
			},
			wantAdded:   []string{"good", "broken"},
			wantRemoved: []string{"good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing := &listingSchedules{}
			periodic := &recordingPeriodic{failFor: tt.failFor}
			scheduler := NewScheduler(listing, periodic)

			for _, pass := range tt.passes {
				listing.schedules = pass
				_ = scheduler.Reconcile(t.Context())
			}

			assert.Equal(t, tt.wantAdded, periodic.added)
			assert.Equal(t, tt.wantRemoved, periodic.removed)
		})
	}
}

// armed is a schedule that should be registered.
func armed(id, cron string) schedulestore.Schedule {
	return schedulestore.Schedule{ID: id, Name: id, Cron: cron, Enabled: true}
}

// paused is the same schedule with its switch off.
func paused(id, cron string) schedulestore.Schedule {
	s := armed(id, cron)
	s.Enabled = false

	return s
}

// renamed changes only the display label, which the periodic job does not read.
func renamed(s schedulestore.Schedule, name string) schedulestore.Schedule {
	s.Name = name

	return s
}

// inZone changes the zone the expression is read in, which the periodic job
// does read.
func inZone(s schedulestore.Schedule, timezone string) schedulestore.Schedule {
	s.Timezone = timezone

	return s
}

// listingSchedules answers the listing each pass reads.
type listingSchedules struct {
	schedules []schedulestore.Schedule
	err       error
}

func (l *listingSchedules) List(context.Context) ([]schedulestore.Schedule, error) {
	return l.schedules, l.err
}

func (l *listingSchedules) Get(context.Context, string) (*schedulestore.Schedule, error) {
	return nil, nil
}

// recordingPeriodic stands in for River's periodic job bundle, recording what it
// was told rather than running anything.
type recordingPeriodic struct {
	added   []string
	removed []string
	// failFor is the schedule id registration is refused for, standing in for a
	// River that will not take one particular job.
	failFor string
}

func (r *recordingPeriodic) Add(id string, job *river.PeriodicJob) error {
	if job == nil {
		return errors.New("a registration with no job to run")
	}

	r.added = append(r.added, id)

	if id == r.failFor {
		return fmt.Errorf("refusing to register %q", id)
	}

	return nil
}

func (r *recordingPeriodic) Remove(id string) {
	r.removed = append(r.removed, id)
}

// TestOccurrenceAt covers which minute a firing is filed under. River constructs
// the job as the schedule fires, which can land either side of the boundary, and
// naming the wrong minute is not a cosmetic error. The run collides with the
// previous one on the unique index and vanishes with nothing recorded.
func TestOccurrenceAt(t *testing.T) {
	minute := time.Date(2026, time.July, 28, 9, 34, 0, 0, time.UTC)

	tests := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{
			name: "exactly on the boundary",
			at:   minute,
			want: minute,
		},
		{
			name: "a hair late, which is the ordinary case",
			at:   minute.Add(7 * time.Millisecond),
			want: minute,
		},
		{
			// Rounding down here would file the run under 09:33 and lose it to the
			// previous minute's job.
			name: "a hair early, which rounding down would misfile",
			at:   minute.Add(-2 * time.Millisecond),
			want: minute,
		},
		{
			name: "well into the minute still belongs to it",
			at:   minute.Add(29 * time.Second),
			want: minute,
		},
		{
			name: "and past the halfway point belongs to the next",
			at:   minute.Add(31 * time.Second),
			want: minute.Add(time.Minute),
		},
		{
			// The job is built when the enqueuer gets to it, which is not always
			// promptly. A leader election, a paused process, or a machine coming out
			// of sleep all put the construction well after the moment the schedule
			// fired, and the firing still belongs to the minute it was due.
			name: "a firing built a minute late still belongs to the minute it was due",
			at:   minute.Add(45 * time.Second),
			want: minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, occurrenceAt(tt.at).UTC())
		})
	}
}
