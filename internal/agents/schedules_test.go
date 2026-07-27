package agents

import (
	"testing"
	"time"

	"claude-agents/internal/store/schedule"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseCron covers what the scheduler will accept. The proto checks that an
// expression has five fields. This is the parse that decides whether those fields
// mean anything.
func TestParseCron(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		timezone   string
		wantErr    bool
	}{
		{
			name:       "a weekday morning",
			expression: "0 9 * * 1-5",
		},
		{
			name:       "a step",
			expression: "*/15 * * * *",
		},
		{
			name:       "a named zone is applied",
			expression: "0 9 * * *",
			timezone:   "America/New_York",
		},
		{
			name:       "an hour that does not exist",
			expression: "0 99 * * *",
			wantErr:    true,
		},
		{
			name:       "a field that is not a number",
			expression: "0 nine * * *",
			wantErr:    true,
		},
		{
			name:       "a zone nobody has heard of",
			expression: "0 9 * * *",
			timezone:   "Mars/Olympus",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseCron(tt.expression, tt.timezone)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrBadCron)
				assert.Nil(t, parsed)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.True(t, parsed.Next(time.Now()).After(time.Now()), "a parsed schedule has a next occurrence")
		})
	}
}

// TestNextRun covers what the panel renders beside a schedule. A paused one has
// no next run, and neither does one whose expression stopped parsing.
func TestNextRun(t *testing.T) {
	// A fixed instant, so the expected occurrence is stated rather than computed
	// the same way the code under test computes it.
	from := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		schedule schedule.Schedule
		want     time.Time
	}{
		{
			name:     "the next hour",
			schedule: schedule.Schedule{Cron: "0 * * * *", Timezone: "UTC", Enabled: true},
			want:     time.Date(2026, time.July, 27, 11, 0, 0, 0, time.UTC),
		},
		{
			name:     "tomorrow morning, since today's has passed",
			schedule: schedule.Schedule{Cron: "0 9 * * *", Timezone: "UTC", Enabled: true},
			want:     time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC),
		},
		{
			name:     "a paused schedule has no next run",
			schedule: schedule.Schedule{Cron: "0 * * * *", Timezone: "UTC"},
		},
		{
			name:     "nor does one that cannot be parsed",
			schedule: schedule.Schedule{Cron: "0 99 * * *", Timezone: "UTC", Enabled: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextRun(tt.schedule, from)

			if tt.want.IsZero() {
				assert.True(t, got.IsZero())
				return
			}

			assert.Equal(t, tt.want, got.UTC())
		})
	}
}

// TestNewSchedule covers what the service does to a config on the way in. It
// trims, expands the launch directory, and refuses an expression that could never
// fire.
func TestNewSchedule(t *testing.T) {
	tests := []struct {
		name     string
		cfg      schedule.Config
		wantErr  error
		wantName string
		wantCwd  string
	}{
		{
			name:     "a schedule is stored as written",
			cfg:      schedule.Config{Name: "morning-ci", Cron: "0 9 * * 1-5", Prompt: "check CI", Cwd: "/work/api"},
			wantName: "morning-ci",
			wantCwd:  "/work/api",
		},
		{
			name:     "surrounding whitespace is trimmed",
			cfg:      schedule.Config{Name: "  spaced  ", Cron: "  0 9 * * *  ", Prompt: " go ", Cwd: "/work/api"},
			wantName: "spaced",
			wantCwd:  "/work/api",
		},
		{
			name:    "an unparseable expression never reaches the store",
			cfg:     schedule.Config{Name: "broken", Cron: "0 99 * * *", Prompt: "go", Cwd: "/work/api"},
			wantErr: ErrBadCron,
		},
		{
			name:    "nor does an unknown timezone",
			cfg:     schedule.Config{Name: "elsewhere", Cron: "0 9 * * *", Timezone: "Mars/Olympus", Prompt: "go", Cwd: "/work/api"},
			wantErr: ErrBadCron,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			svc := NewScheduleService(ScheduleParams{Schedules: st.Schedules})

			created, err := svc.NewSchedule(t.Context(), tt.cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				listed, listErr := st.Schedules.List(t.Context())
				require.NoError(t, listErr)
				assert.Empty(t, listed, "a refused schedule is not written")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, tt.wantName, created.Name)
			assert.Equal(t, tt.wantCwd, created.Cwd)
		})
	}
}

// TestScheduleNotFound covers the ids the mutating methods refuse. An id nothing
// holds is not-found rather than a silent success, so a caller editing a schedule
// someone else deleted hears about it.
func TestScheduleNotFound(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T, svc *ScheduleService) error
	}{
		{
			name: "updating",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.UpdateSchedule(t.Context(), missingScheduleID,
					schedule.Config{Name: "x", Cron: "0 9 * * *", Prompt: "go", Cwd: "/work/api"})
			},
		},
		{
			name: "pausing",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.SetScheduleEnabled(t.Context(), missingScheduleID, false)
			},
		},
		{
			name: "deleting",
			call: func(t *testing.T, svc *ScheduleService) error {
				return svc.DeleteSchedule(t.Context(), missingScheduleID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			svc := NewScheduleService(ScheduleParams{Schedules: st.Schedules})

			require.ErrorIs(t, tt.call(t, svc), ErrScheduleNotFound)
		})
	}
}

// missingScheduleID is a well-formed uuid no schedule holds. A malformed one
// would be refused before the lookup and prove something else.
const missingScheduleID = "00000000-0000-0000-0000-000000000000"
