package internaljobs

import (
	"testing"
	"time"

	"komrad/internal/schedules"

	robfig "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var neverOccurring = []string{"0 0 30 2 *", "0 0 31 2 *", "0 0 31 4 *"}

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
			name: "even two hours late, after the machine slept through it",
			cron: "0 9 * * *",
			at:   nine.Add(2 * time.Hour),
			want: nine,
		},
		{
			name: "and a yearly one built weeks late belongs to its day",
			cron: "0 9 28 7 *",
			at:   nine.Add(40 * 24 * time.Hour),
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

// TestLaunchArgsForLateFiringLeavesTheNextOne covers a firing built hours late, whose arguments
// must differ from the next morning's or River drops that one as a duplicate.
func TestLaunchArgsForLateFiringLeavesTheNextOne(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	cron, err := schedules.ParseCron("0 9 * * *", "UTC")
	require.NoError(t, err)

	late, _ := LaunchArgsFor("s1", cron, nine.Add(2*time.Hour))
	next, _ := LaunchArgsFor("s1", cron, nine.Add(24*time.Hour))

	assert.NotEqual(t, late, next)
}
