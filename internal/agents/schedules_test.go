package agents

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/store/schedule"
	"weave/internal/store/workflow"

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
	}{
		{
			name:     "a schedule is stored as written",
			cfg:      schedule.Config{Name: "morning-ci", Cron: "0 9 * * 1-5", Prompt: "check CI"},
			wantName: "morning-ci",
		},
		{
			name:     "surrounding whitespace is trimmed",
			cfg:      schedule.Config{Name: "  spaced  ", Cron: "  0 9 * * *  ", Prompt: " go "},
			wantName: "spaced",
		},
		{
			name:    "an unparseable expression never reaches the store",
			cfg:     schedule.Config{Name: "broken", Cron: "0 99 * * *", Prompt: "go"},
			wantErr: ErrBadCron,
		},
		{
			name:    "nor does an unknown timezone",
			cfg:     schedule.Config{Name: "elsewhere", Cron: "0 9 * * *", Timezone: "Mars/Olympus", Prompt: "go"},
			wantErr: ErrBadCron,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			svc := NewScheduleService(ScheduleParams{Schedules: st.Schedules, Workflows: st.Workflows})

			workflowID := st.SeedWorkflow(t, workflow.Config{Name: "backend"}).ID
			cfg := tt.cfg
			cfg.WorkflowID = workflowID

			created, err := svc.NewSchedule(t.Context(), cfg)
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
			assert.Equal(t, workflowID, created.WorkflowID, "a schedule always names the workflow it launches into")
		})
	}
}

// TestScheduleWorkflowMustExist covers the workflow a schedule launches into. A
// schedule reads its directory and flags off that workflow as it fires, so one
// pointing at a workflow that is not there could never launch anything and is
// refused when it is written rather than every time it fires.
func TestScheduleWorkflowMustExist(t *testing.T) {
	// archiveWorkflow is the case the foreign key cannot catch. A workflow is only
	// ever soft-deleted, so the row is still there for the constraint while every
	// read of it comes back empty.
	tests := []struct {
		name            string
		archiveWorkflow bool
		workflowID      string
	}{
		{
			name:            "a workflow that has been deleted",
			archiveWorkflow: true,
		},
		{
			name:       "a well-formed id no workflow holds",
			workflowID: missingWorkflowID,
		},
		{
			name:       "an id that is not an id at all",
			workflowID: "not-a-uuid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			svc := NewScheduleService(ScheduleParams{Schedules: st.Schedules, Workflows: st.Workflows})

			// A live workflow to move an existing schedule off of, so the update case
			// starts from a schedule that is valid.
			live := st.SeedWorkflow(t, workflow.Config{Name: "backend"}).ID
			existing, err := svc.NewSchedule(ctx, schedule.Config{
				Name: "nightly", Cron: "0 9 * * *", Prompt: "go", WorkflowID: live,
			})
			require.NoError(t, err)

			target := tt.workflowID
			if tt.archiveWorkflow {
				target = st.SeedWorkflow(t, workflow.Config{Name: "gone"}).ID
				require.NoError(t, st.Workflows.Archive(ctx, target))
			}

			_, err = svc.NewSchedule(ctx, schedule.Config{
				Name: "morning", Cron: "0 9 * * *", Prompt: "go", WorkflowID: target,
			})
			require.ErrorIs(t, err, ErrWorkflowNotFound, "a schedule is not created into a workflow that is not there")

			err = svc.UpdateSchedule(ctx, existing.ID, schedule.Config{
				Name: "nightly", Cron: "0 9 * * *", Prompt: "go", WorkflowID: target,
			})
			require.ErrorIs(t, err, ErrWorkflowNotFound, "nor is one moved into it")

			listed, err := st.Schedules.List(ctx)
			require.NoError(t, err)
			require.Len(t, listed, 1, "only the schedule that named a live workflow was written")
			assert.Equal(t, live, listed[0].WorkflowID, "the refused update left the schedule where it was")
		})
	}
}

// TestScheduleIsNotBlockedByARunThatNeverStarted covers the count a firing
// consults before it launches, driven through the reconciler that decides the
// states it counts.
//
// A schedule runs one agent at a time, so a firing skips its tick while the last
// run is still going. The trap is a run that never got going at all: an agent
// exiting the moment it is typed leaves an ordinary shell in a pane that is still
// the agent's own, so the reconciler observes an idle frame forever and the agent
// keeps the state it was recorded with. Counting that as work in flight is what
// turns one failed launch into a schedule that never fires again, and says
// nothing about it anywhere.
func TestScheduleIsNotBlockedByARunThatNeverStarted(t *testing.T) {
	tests := []struct {
		name string
		// state is what the schedule's last run is sitting in.
		state string
		// frame is what its pane shows on every pass.
		frame string
		want  int64
	}{
		{
			name:  "a run that never started leaves the schedule free to fire",
			state: agentstore.StateIdle,
			frame: idleFrame,
			want:  0,
		},
		{
			name:  "a run still working holds the next tick off",
			state: agentstore.StateThinking,
			frame: busyFrame,
			want:  1,
		},
		{
			name:  "so does one waiting on an answer, which is work still owed",
			state: agentstore.StateIdle,
			frame: waitFrame,
			want:  1,
		},
		{
			name:  "a run that finished does not",
			state: agentstore.StateIdle,
			frame: idleFrame,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			schedule := st.SeedSchedule(t, schedule.Config{Name: "nightly", Cron: "0 2 * * *", Prompt: "sweep"})
			run := st.SeedAgent(t, agentstore.Agent{
				Name: "nightly-run", Pane: "%1", State: tt.state,
				WorkflowID: schedule.WorkflowID, ScheduleID: &schedule.ID,
			})

			// The pane is still stamped with the agent, which is what a CLI that
			// exited back to its shell looks like. Only the frame says otherwise.
			tmux := newFakeTmux()
			tmux.owners["%1"] = run.ID
			tmux.captures["%1"] = tt.frame

			// More passes than the idle debounce needs, so whatever the reconciler is
			// going to settle this agent on, it has settled on.
			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			for range reconcileMisses + 2 {
				require.NoError(t, rec.Reconcile(ctx))
			}

			count, err := st.Agents.CountLiveForSchedule(ctx, schedule.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, count, "what the next firing sees")
		})
	}
}

// missingWorkflowID is a well-formed uuid no workflow holds, so a lookup for it
// reaches the store and comes back empty rather than being refused for its shape.
const missingWorkflowID = "00000000-0000-0000-0000-0000000000ff"

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
					schedule.Config{Name: "x", Cron: "0 9 * * *", Prompt: "go", WorkflowID: "w1"})
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
				return svc.ArchiveSchedule(t.Context(), missingScheduleID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			svc := NewScheduleService(ScheduleParams{Schedules: st.Schedules, Workflows: st.Workflows})

			require.ErrorIs(t, tt.call(t, svc), ErrScheduleNotFound)
		})
	}
}

// missingScheduleID is a well-formed uuid no schedule holds. A malformed one
// would be refused before the lookup and prove something else.
const missingScheduleID = "00000000-0000-0000-0000-000000000000"
