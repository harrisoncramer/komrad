package schedule

import (
	"testing"
	"time"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var nine = time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

type firing struct {
	scheduleID string
	hour       int
	attempt    int
	outcome    *RunOutcome
}

type wantRun struct {
	hour      int
	state     RunState
	attempt   int
	finalized bool
	message   string
}

// TestListRuns pins what the run record holds after each way an attempt at a firing can go.
func TestListRuns(t *testing.T) {
	tests := []struct {
		name    string
		firings []firing
		limit   int
		want    []wantRun
	}{
		{
			name: "a schedule that never fired has no runs",
			want: []wantRun{},
		},
		{
			name: "a retryable attempt is left open",
			firings: []firing{
				{
					attempt: 1,
					outcome: &RunOutcome{
						State:   RunStateRetryable,
						Message: "no server running",
					},
				},
			},
			want: []wantRun{
				{
					state:   RunStateRetryable,
					attempt: 1,
					message: "no server running",
				},
			},
		},
		{
			name: "a retry of the same firing reopens its one run",
			firings: []firing{
				{
					attempt: 1,
					outcome: &RunOutcome{
						State:   RunStateRetryable,
						Message: "no server running",
					},
				},
				{
					attempt: 2,
				},
			},
			want: []wantRun{
				{
					state:   RunStateRunning,
					attempt: 2,
					message: "no server running",
				},
			},
		},
		{
			name: "a cancelled firing is finalized with its reason",
			firings: []firing{
				{
					attempt: 1,
					outcome: &RunOutcome{
						State:   RunStateCancelled,
						Message: "schedule is paused",
					},
				},
			},
			want: []wantRun{
				{
					state:     RunStateCancelled,
					attempt:   1,
					finalized: true,
					message:   "schedule is paused",
				},
			},
		},
		{
			name: "runs come back newest first, as many as the limit",
			firings: []firing{
				{
					attempt: 1,
					outcome: &RunOutcome{State: RunStateCompleted},
				},
				{
					hour:    1,
					attempt: 1,
					outcome: &RunOutcome{State: RunStateCompleted},
				},
				{
					hour:    2,
					attempt: 1,
				},
			},
			limit: 2,
			want: []wantRun{
				{
					hour:    2,
					state:   RunStateRunning,
					attempt: 1,
				},
				{
					hour:      1,
					state:     RunStateCompleted,
					attempt:   1,
					finalized: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			fired := seed(t, st, Config{
				Name:   "morning-ci",
				Cron:   "0 9 * * *",
				Prompt: "check CI",
			})
			other := seed(t, st, Config{
				Name:   "nightly",
				Cron:   "0 2 * * *",
				Prompt: "sweep",
			})
			startRun(t, st, firing{
				scheduleID: other.ID,
				attempt:    1,
			})

			for _, fire := range tt.firings {
				fire.scheduleID = fired.ID
				startRun(t, st, fire)
			}

			limit := tt.limit
			if limit == 0 {
				limit = 10
			}

			runs, err := st.ListRuns(ctx, fired.ID, limit)
			require.NoError(t, err)

			got := xslice.Map(runs, func(run Run) wantRun {
				return wantRun{
					hour:      int(run.ScheduledFor.Sub(nine).Hours()),
					state:     run.State,
					attempt:   run.Attempt,
					finalized: run.FinalizedAt != nil,
					message:   run.Message,
				}
			})
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFinishRunPrunesTheOldestRuns pins that a schedule keeps only its newest runs.
func TestFinishRunPrunesTheOldestRuns(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created := seed(t, st, Config{
		Name:   "every-minute",
		Cron:   "* * * * *",
		Prompt: "poll",
	})

	for hour := range runsKept + 1 {
		startRun(t, st, firing{
			scheduleID: created.ID,
			hour:       hour,
			attempt:    1,
			outcome:    &RunOutcome{State: RunStateCompleted},
		})
	}

	runs, err := st.ListRuns(ctx, created.ID, runsKept+10)
	require.NoError(t, err)
	require.Len(t, runs, runsKept)
	assert.Equal(t, nine.Add(time.Hour), runs[len(runs)-1].ScheduledFor.UTC(), "the oldest run is the one dropped")
}

// TestRunsRideOnTheSchedule pins which reads carry a schedule's newest run and which carry its recent ones.
func TestRunsRideOnTheSchedule(t *testing.T) {
	tests := []struct {
		name       string
		read       func(t *testing.T, st *fixture, id string) []Schedule
		wantRecent int
	}{
		{
			name: "a read of one schedule carries only its newest run",
			read: func(t *testing.T, st *fixture, id string) []Schedule {
				got, err := st.Get(t.Context(), id)
				require.NoError(t, err)

				return []Schedule{*got}
			},
		},
		{
			name: "a page of schedules carries the newest run of each",
			read: func(t *testing.T, st *fixture, _ string) []Schedule {
				page, err := st.ListPage(t.Context(), 10, nil)
				require.NoError(t, err)

				return page
			},
		},
		{
			name: "the watched set carries the recent runs too",
			read: func(t *testing.T, st *fixture, _ string) []Schedule {
				watched, err := st.ListWatched(t.Context(), nil)
				require.NoError(t, err)

				return watched
			},
			wantRecent: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)

			fired := seed(t, st, Config{
				Name:   "morning-ci",
				Cron:   "0 9 * * *",
				Prompt: "check CI",
			})
			quiet := seed(t, st, Config{
				Name:   "nightly",
				Cron:   "0 2 * * *",
				Prompt: "sweep",
			})
			for hour := range 3 {
				startRun(t, st, firing{
					scheduleID: fired.ID,
					hour:       hour,
					attempt:    1,
					outcome:    &RunOutcome{State: RunStateCompleted},
				})
			}

			byID := indexSchedulesByID(tt.read(t, st, fired.ID))

			require.NotNil(t, byID[fired.ID].LatestRun)
			assert.Equal(t, nine.Add(2*time.Hour), byID[fired.ID].LatestRun.ScheduledFor.UTC())
			assert.Len(t, byID[fired.ID].RecentRuns, tt.wantRecent)
			if unfired, ok := byID[quiet.ID]; ok {
				assert.Nil(t, unfired.LatestRun, "a schedule that never fired carries no run")
				assert.Empty(t, unfired.RecentRuns)
			}
		})
	}
}

func startRun(t *testing.T, st *fixture, fire firing) {
	t.Helper()

	occurrence := RunOccurrence{
		ScheduleID:   fire.scheduleID,
		ScheduledFor: nine.Add(time.Duration(fire.hour) * time.Hour),
	}
	require.NoError(t, st.StartRun(t.Context(), StartRunParams{
		Occurrence:  occurrence,
		Attempt:     fire.attempt,
		MaxAttempts: 5,
	}))

	if fire.outcome != nil {
		require.NoError(t, st.FinishRun(t.Context(), occurrence, *fire.outcome))
	}
}

func indexSchedulesByID(schedules []Schedule) map[string]Schedule {
	byID := make(map[string]Schedule, len(schedules))
	for _, schedule := range schedules {
		byID[schedule.ID] = schedule
	}

	return byID
}
