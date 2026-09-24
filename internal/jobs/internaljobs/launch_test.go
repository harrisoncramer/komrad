package internaljobs

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/internal/tmuxtest"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudeHarnessID = "claude"

var nine = time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

// TestLaunchAgentWorker pins what one firing launches, and how the run and the job record each way it can end.
func TestLaunchAgentWorker(t *testing.T) {
	tests := []struct {
		name            string
		deleted         bool
		paused          bool
		hideAgents      bool
		missingCwd      bool
		busy            bool
		crowded         bool
		windowErr       error
		attempt         int
		wantCancel      bool
		wantErr         bool
		wantReason      string
		wantRuns        []schedulestore.RunState
		wantAgentHidden *bool
	}{
		{
			name:       "a schedule deleted since the job was queued cancels",
			deleted:    true,
			wantCancel: true,
			wantReason: "no longer exists",
			wantRuns:   []schedulestore.RunState{},
		},
		{
			name:       "a schedule paused since the job was queued cancels",
			paused:     true,
			wantCancel: true,
			wantReason: "is paused",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCancelled},
		},
		{
			name:       "a schedule whose last agent is still working skips this firing",
			busy:       true,
			wantCancel: true,
			wantReason: "still has 1 agent(s) running",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCancelled},
		},
		{
			name:       "a workflow directory that cannot be opened cancels",
			missingCwd: true,
			wantCancel: true,
			wantReason: agents.ErrBadDirectory.Error(),
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCancelled},
		},
		{
			name:       "a panel with no room for another agent cancels",
			crowded:    true,
			wantCancel: true,
			wantReason: agents.ErrTooManyAgents.Error(),
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCancelled},
		},
		{
			name:       "a failure that might pass is left for River to retry",
			windowErr:  errors.New("no server running"),
			wantErr:    true,
			wantReason: "no server running",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateRetryable},
		},
		{
			name:       "and on the last attempt is recorded as discarded",
			windowErr:  errors.New("no server running"),
			attempt:    5,
			wantErr:    true,
			wantReason: "no server running",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateDiscarded},
		},
		{
			name:            "a schedule hiding its runs launches a hidden agent",
			hideAgents:      true,
			wantRuns:        []schedulestore.RunState{schedulestore.RunStateCompleted},
			wantAgentHidden: xptr.Ptr(true),
		},
		{
			name:            "an armed schedule launches its agent",
			wantRuns:        []schedulestore.RunState{schedulestore.RunStateCompleted},
			wantAgentHidden: xptr.Ptr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()

			cwd := t.TempDir()
			if tt.missingCwd {
				cwd = filepath.Join(cwd, "gone")
			}
			workflow := stores.SeedWorkflow(t, workflowstore.Config{
				Name:    "nightly",
				Harness: claudeHarnessID,
				Cwd:     cwd,
			})
			schedule := stores.SeedSchedule(t, schedulestore.Config{
				Name:       "morning-ci",
				Cron:       "0 9 * * *",
				Prompt:     "check CI",
				Enabled:    !tt.paused,
				HideAgents: tt.hideAgents,
				WorkflowID: workflow.ID,
			})
			if tt.deleted {
				require.NoError(t, stores.Schedules.Delete(ctx, schedule.ID))
			}
			if tt.busy {
				stores.SeedAgent(t, agentstore.Agent{
					Name:       "morning-ci",
					State:      agentstore.StateThinking,
					WorkflowID: workflow.ID,
					ScheduleID: xptr.Ptr(schedule.ID),
				})
			}

			maxAgents := 0
			if tt.crowded {
				stores.SeedAgent(t, agentstore.Agent{Name: "elsewhere"})
				maxAgents = 1
			}

			terminal := tmuxtest.NewFake()
			terminal.NewWindowErr = tt.windowErr
			worker := NewLaunchAgentWorker(NewLaunchAgentWorkerParams{
				Schedules: stores.Schedules,
				Runs:      stores.Schedules,
				Launcher: newAgentService(t, newAgentServiceParams{
					stores:    stores,
					terminal:  terminal,
					maxAgents: maxAgents,
				}),
				Running: stores.Agents,
			})

			err := worker.Work(ctx, &river.Job[LaunchAgentArgs]{
				JobRow: &rivertype.JobRow{
					Attempt:     max(tt.attempt, 1),
					MaxAttempts: 5,
				},
				Args: LaunchAgentArgs{
					ScheduleID:   schedule.ID,
					ScheduledFor: nine,
				},
			})

			switch {
			case tt.wantCancel:
				require.Error(t, err)
				assert.True(t, isCancel(err), "a job that cannot succeed cancels rather than being retried")
			case tt.wantErr:
				require.Error(t, err)
				assert.False(t, isCancel(err), "a failure that might pass is left for River to retry")
			default:
				require.NoError(t, err)
			}
			if tt.wantReason != "" {
				assert.Contains(t, err.Error(), tt.wantReason, "the error River records on the job says why")
			}

			runs, err := stores.Schedules.ListRuns(ctx, schedule.ID, 10)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRuns, xslice.Map(runs, func(run schedulestore.Run) schedulestore.RunState {
				return run.State
			}))
			if len(runs) > 0 && tt.wantReason != "" {
				assert.Contains(t, runs[0].Message, tt.wantReason, "the run records why")
				assert.NotContains(t, runs[0].Message, "JobCancelError", "in the worker's own words")
			}

			if tt.wantAgentHidden == nil {
				return
			}

			launched := listScheduleAgents(t, stores, schedule.ID)
			require.Len(t, launched, 1)
			assert.Equal(t, "morning-ci", launched[0].Name, "the agent is named for the schedule")
			assert.Equal(t, "check CI", launched[0].Prompt)
			assert.Equal(t, workflow.ID, launched[0].WorkflowID)
			assert.Equal(t, cwd, launched[0].Cwd, "the directory is the workflow's")
			assert.Equal(t, *tt.wantAgentHidden, launched[0].Hidden)
			require.NotNil(t, launched[0].IdempotencyKey)
			assert.Equal(t, "schedule:"+schedule.ID+":2026-07-28T09:00:00Z", *launched[0].IdempotencyKey)
		})
	}
}

// TestLaunchAgentWorkerLaunchesOneAgentPerFiring pins the idempotency key as all that stops a River retry making a second agent.
func TestLaunchAgentWorkerLaunchesOneAgentPerFiring(t *testing.T) {
	tests := []struct {
		name              string
		occurrences       []time.Time
		promoteAfterFirst bool
		wantSkipped       bool
		wantAgents        int
		wantRuns          []schedulestore.RunState
	}{
		{
			name:              "a later firing skips while the last agent is still working",
			occurrences:       []time.Time{nine, nine.Add(24 * time.Hour)},
			promoteAfterFirst: true,
			wantSkipped:       true,
			wantAgents:        1,
			wantRuns:          []schedulestore.RunState{schedulestore.RunStateCancelled, schedulestore.RunStateCompleted},
		},
		{
			name:        "working the same firing twice launches one agent",
			occurrences: []time.Time{nine, nine},
			wantAgents:  1,
			wantRuns:    []schedulestore.RunState{schedulestore.RunStateCompleted},
		},
		{
			name:        "the same firing written in another zone launches one agent",
			occurrences: []time.Time{nine, nine.In(time.FixedZone("elsewhere", 5*60*60))},
			wantAgents:  1,
			wantRuns:    []schedulestore.RunState{schedulestore.RunStateCompleted},
		},
		{
			name:        "a later firing launches once the last agent is no longer working",
			occurrences: []time.Time{nine, nine.Add(24 * time.Hour)},
			wantAgents:  2,
			wantRuns:    []schedulestore.RunState{schedulestore.RunStateCompleted, schedulestore.RunStateCompleted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			workflow := stores.SeedWorkflow(t, workflowstore.Config{
				Name:    "nightly",
				Harness: claudeHarnessID,
				Cwd:     t.TempDir(),
			})
			schedule := stores.SeedSchedule(t, schedulestore.Config{
				Name:       "morning-ci",
				Cron:       "0 9 * * *",
				Prompt:     "check CI",
				Enabled:    true,
				WorkflowID: workflow.ID,
			})

			worker := NewLaunchAgentWorker(NewLaunchAgentWorkerParams{
				Schedules: stores.Schedules,
				Runs:      stores.Schedules,
				Launcher: newAgentService(t, newAgentServiceParams{
					stores:   stores,
					terminal: tmuxtest.NewFake(),
				}),
				Running: stores.Agents,
			})

			for i, occurrence := range tt.occurrences {
				err := worker.Work(t.Context(), &river.Job[LaunchAgentArgs]{
					JobRow: &rivertype.JobRow{
						Attempt:     1,
						MaxAttempts: 5,
					},
					Args: LaunchAgentArgs{
						ScheduleID:   schedule.ID,
						ScheduledFor: occurrence,
					},
				})

				last := i == len(tt.occurrences)-1
				if last && tt.wantSkipped {
					require.Error(t, err)
					assert.True(t, isCancel(err), "a firing that declines to launch cancels rather than failing")
				} else {
					require.NoError(t, err)
				}

				if i == 0 && tt.promoteAfterFirst {
					for _, agent := range listScheduleAgents(t, stores, schedule.ID) {
						require.NoError(t, stores.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateThinking))
					}
				}
			}

			assert.Len(t, listScheduleAgents(t, stores, schedule.ID), tt.wantAgents)

			runs, err := stores.Schedules.ListRuns(t.Context(), schedule.ID, 10)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRuns, xslice.Map(runs, func(run schedulestore.Run) schedulestore.RunState {
				return run.State
			}))
		})
	}
}

type newAgentServiceParams struct {
	stores    *storetest.Stores
	terminal  *tmuxtest.Fake
	maxAgents int
}

func newAgentService(t *testing.T, params newAgentServiceParams) *agents.LaunchService {
	t.Helper()

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))
	require.NoError(t, err)

	return agents.NewLaunchService(agents.NewLaunchServiceParams{
		Tmux:      params.terminal,
		Agents:    params.stores.Agents,
		Reviews:   reviewstore.NewReviewStore(params.stores.Pool.GetDB()),
		Workflows: params.stores.Workflows,
		Tx:        params.stores.Pool,
		Launcher: agents.NewLauncher(agents.NewLauncherParams{
			Tmux: params.terminal,
			Addr: "127.0.0.1:8787",
		}),
		Harnesses:          registry,
		UserDefaultHarness: claudeHarnessID,
		MaxAgents:          params.maxAgents,
	})
}

func listScheduleAgents(t *testing.T, stores *storetest.Stores, scheduleID string) []agentstore.Agent {
	t.Helper()

	listed, err := stores.Agents.List(t.Context())
	require.NoError(t, err)

	return xslice.Filter(listed, func(agent agentstore.Agent) bool {
		return agent.ScheduleID != nil && *agent.ScheduleID == scheduleID
	})
}

func isCancel(err error) bool {
	var cancel *river.JobCancelError

	return errors.As(err, &cancel)
}
