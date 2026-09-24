package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/schedules"
	"weave/internal/store"
	schedulestore "weave/internal/store/schedule"

	"github.com/riverqueue/river"
	robfig "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var neverOccurring = []string{"0 0 30 2 *", "0 0 31 2 *", "0 0 31 4 *"}

// TestSchedulerReconcile covers which schedules a pass registers and which it takes off.
func TestSchedulerReconcile(t *testing.T) {
	tests := []struct {
		name        string
		passes      [][]schedulestore.Schedule
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

// TestSchedulerReconcileFailure covers a listing the reconciler could not read surfacing rather
// than reading as every schedule having been deleted.
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

// TestSchedulerReconcileSurvivesAFailedRegistration covers River refusing one schedule while
// the rest still register and a paused one still comes off.
func TestSchedulerReconcileSurvivesAFailedRegistration(t *testing.T) {
	tests := []struct {
		name        string
		failFor     string
		passes      [][]schedulestore.Schedule
		wantAdded   []string
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

func armed(id, cron string) schedulestore.Schedule {
	return schedulestore.Schedule{
		ID:      id,
		Name:    id,
		Cron:    cron,
		Enabled: true,
	}
}

func paused(id, cron string) schedulestore.Schedule {
	s := armed(id, cron)
	s.Enabled = false

	return s
}

func renamed(s schedulestore.Schedule, name string) schedulestore.Schedule {
	s.Name = name

	return s
}

func inZone(s schedulestore.Schedule, timezone string) schedulestore.Schedule {
	s.Timezone = timezone

	return s
}

type listingSchedules struct {
	schedules []schedulestore.Schedule
	err       error
}

func (l *listingSchedules) ListWatched(context.Context, []string) ([]schedulestore.Schedule, error) {
	return l.schedules, l.err
}

func (l *listingSchedules) Get(_ context.Context, id string) (*schedulestore.Schedule, error) {
	return nil, fmt.Errorf("%w: schedule %q", store.ErrNotFound, id)
}

type recordingPeriodic struct {
	added   []string
	removed []string
	failFor string
}

func (r *recordingPeriodic) Add(id string, cron river.PeriodicSchedule) error {
	if cron == nil {
		return errors.New("a registration with nothing to fire on")
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
