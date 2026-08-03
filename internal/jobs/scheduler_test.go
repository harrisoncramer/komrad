package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/agents"
	schedulestore "weave/internal/store/schedule"

	"github.com/riverqueue/river"
	robfig "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// neverOccurring are dates the calendar does not have. They parse, and then the library
// searches five years, finds nothing, and reports the zero time. They are built straight
// from the library rather than through agents.ParseCron, which refuses them, since what is
// under test is what happens when one reaches the scheduler anyway.
var neverOccurring = []string{"0 0 30 2 *", "0 0 31 2 *", "0 0 31 4 *"}

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

// TestOccurrenceFor covers which firing a job is filed under. River builds the job
// when its enqueuer gets to the schedule, which is not always promptly, and naming
// the wrong firing is not a cosmetic error. The run either collides with the previous
// one on the unique index and vanishes with nothing recorded, or takes the name of a
// firing that has not happened yet and blocks the one that follows.
func TestOccurrenceFor(t *testing.T) {
	// Nine in the morning is both the hourly and the daily occurrence, so the rows
	// below share it.
	minute := time.Date(2026, time.July, 28, 9, 34, 0, 0, time.UTC)
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		cron string
		// at is when the job was built, which is the firing's own moment only in the
		// case where nothing delayed the enqueuer.
		at   time.Time
		want time.Time
	}{
		{
			// A firing can land a hair before the boundary. Taking the occurrence
			// before it would collide with a run that already happened.
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
			// Past what is worth running, so there is nothing to name. Naming the
			// firing still to come is what this guards against, since the run would
			// take that firing's place and no agent would start when it arrived.
			name: "one built later than staleness allows names nothing",
			cron: "0 9 * * *",
			at:   nine.Add(90 * time.Minute),
			want: time.Time{},
		},
		{
			// The far edge is exclusive, because the walk starts strictly after where
			// it is pointed.
			name: "and so does one built exactly a staleness late",
			cron: "0 9 * * *",
			at:   nine.Add(staleness),
			want: time.Time{},
		},
		{
			// A laptop shut overnight and opened the next morning. Yesterday's firing
			// is gone rather than run hours after anyone wanted it.
			name: "nor does a machine opened the following morning run yesterday's firing",
			cron: "0 9 * * *",
			at:   nine.Add(23 * time.Hour),
			want: time.Time{},
		},
		{
			// Lateness is the enqueuer's, not the schedule's, so an infrequent
			// schedule is reached on the same window as a daily one.
			name: "a yearly schedule built promptly names its own firing",
			cron: "0 9 1 1 *",
			at:   time.Date(2026, time.January, 1, 9, 30, 0, 0, time.UTC),
			want: time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC),
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
			cron, err := agents.ParseCron(tt.cron, "UTC")
			require.NoError(t, err)

			assert.Equal(t, tt.want, occurrenceFor(cron, tt.at).UTC())
		})
	}
}

// TestLaunchArgsForStaleBuildLeavesTheNextFiringAlone covers why a stale build has to queue
// nothing rather than take the name of the firing to come. The arguments are unique by value
// across every terminal state, so a run wearing that name means the firing's own insert
// carries identical arguments, collapses into it, and starts no agent. The next firing has to
// arrive to its own name and its own idempotency key.
func TestLaunchArgsForStaleBuildLeavesTheNextFiringAlone(t *testing.T) {
	nine := time.Date(2026, time.August, 2, 9, 0, 0, 0, time.UTC)
	tomorrow := nine.AddDate(0, 0, 1)

	cron, err := agents.ParseCron("0 9 * * *", "UTC")
	require.NoError(t, err)

	stale, _ := launchArgsFor("s1", cron, nine.Add(3*time.Hour))
	assert.Nil(t, stale, "a firing nobody was around for is not worth queueing")

	onTime, _ := launchArgsFor("s1", cron, tomorrow.Add(time.Second))
	args, ok := onTime.(LaunchAgentArgs)
	require.True(t, ok, "the next firing still queues")

	assert.Equal(t, tomorrow, args.ScheduledFor.UTC(), "under its own name")
	assert.Equal(t, LaunchKey("s1", tomorrow), LaunchKey("s1", args.ScheduledFor))
}

// TestOccurrenceForNeverOccurs covers the walk over a schedule that has no occurrence.
// Every step of it comes back as the zero time, which is never after the cutoff, so
// without a stop the walk spins inside River's enqueuer and no schedule fires again.
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

// TestLaunchArgsForNeverOccurs covers what such a schedule queues. Nothing, since nil
// arguments are how River is told there is no job to insert.
func TestLaunchArgsForNeverOccurs(t *testing.T) {
	for _, expression := range neverOccurring {
		t.Run(expression, func(t *testing.T) {
			schedule, err := robfig.ParseStandard(expression)
			require.NoError(t, err)

			args, _ := launchArgsFor("s1", schedule, time.Now())
			assert.Nil(t, args)
		})
	}
}

// TestLaunchArgsForOccurrence covers the ordinary case, where the arguments name the
// schedule and the firing they stand for.
func TestLaunchArgsForOccurrence(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	cron, err := agents.ParseCron("0 9 * * *", "UTC")
	require.NoError(t, err)

	args, _ := launchArgsFor("s1", cron, nine.Add(30*time.Minute))
	launch, ok := args.(LaunchAgentArgs)
	require.True(t, ok)

	assert.Equal(t, "s1", launch.ScheduleID)
	assert.Equal(t, nine, launch.ScheduledFor.UTC())
}
