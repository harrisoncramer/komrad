package internaljobs

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/agents"
	"komrad/internal/harness"
	"komrad/internal/harness/claude"
	"komrad/internal/mail"
	"komrad/internal/schedules"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

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
		name         string
		deleted      bool
		paused       bool
		hideAgents   bool
		missingCwd   bool
		busy         bool
		crowded      bool
		windowErr    error
		attempt      int
		wantCancel   bool
		wantErr      bool
		wantReason   string
		wantRuns     []schedulestore.RunState
		wantLaunched bool
		wantHidden   bool
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
			name:         "a schedule hiding its runs launches a hidden agent",
			hideAgents:   true,
			wantRuns:     []schedulestore.RunState{schedulestore.RunStateCompleted},
			wantLaunched: true,
			wantHidden:   true,
		},
		{
			name:         "an armed schedule launches its agent",
			wantRuns:     []schedulestore.RunState{schedulestore.RunStateCompleted},
			wantLaunched: true,
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

			if !tt.wantLaunched {
				return
			}

			launched := listScheduleAgents(t, stores, schedule.ID)
			require.Len(t, launched, 1)
			assert.Equal(t, "morning-ci", launched[0].Name, "the agent is named for the schedule")
			assert.Equal(t, "check CI", launched[0].Prompt)
			assert.Equal(t, workflow.ID, launched[0].WorkflowID)
			assert.Equal(t, cwd, launched[0].Cwd, "the directory is the workflow's")
			assert.Equal(t, tt.wantHidden, launched[0].Hidden)
			require.NotNil(t, launched[0].IdempotencyKey)
			assert.Equal(t, "schedule:"+schedule.ID+":2026-07-28T09:00:00Z", *launched[0].IdempotencyKey)
		})
	}
}

// TestLaunchAgentWorkerLaunchesOneAgentPerFiring pins the idempotency key as all that stops a River retry making a second agent.
func TestLaunchAgentWorkerLaunchesOneAgentPerFiring(t *testing.T) {
	tests := []struct {
		name        string
		occurrences []time.Time
		wantAgents  int
		wantRuns    []schedulestore.RunState
	}{
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

			for _, occurrence := range tt.occurrences {
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
				require.NoError(t, err)
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

// TestLaunchAgentWorkerMailsAnOrchestrator pins what an orchestrator schedule's firing sends, and
// that one whose orchestrator is gone is skipped with the reason on its run.
func TestLaunchAgentWorkerMailsAnOrchestrator(t *testing.T) {
	tests := []struct {
		name       string
		archived   bool
		paused     bool
		twice      bool
		wantReason string
		wantRuns   []schedulestore.RunState
		wantMailed bool
	}{
		{
			name:       "a live orchestrator is mailed the prompt",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCompleted},
			wantMailed: true,
		},
		{
			name:       "working the same firing twice mails once",
			twice:      true,
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCompleted},
			wantMailed: true,
		},
		{
			name:       "a paused schedule mails nothing",
			paused:     true,
			wantReason: "is paused",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCancelled},
		},
		{
			name:       "an orchestrator archived since the schedule was written is skipped",
			archived:   true,
			wantReason: "orchestrator \"lead\" has been archived",
			wantRuns:   []schedulestore.RunState{schedulestore.RunStateCancelled},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()

			workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "nightly", Cwd: t.TempDir()})
			orchestrator := stores.SeedAgent(t, agentstore.Agent{
				Name:       "lead",
				Role:       agentstore.RoleOrchestrator,
				WorkflowID: workflow.ID,
			})
			schedule := stores.SeedSchedule(t, schedulestore.Config{
				Name:                "morning-ci",
				Cron:                "0 9 * * *",
				Prompt:              "check CI",
				Enabled:             !tt.paused,
				WorkflowID:          workflow.ID,
				Kind:                schedulestore.KindOrchestrator,
				OrchestratorAgentID: &orchestrator.ID,
			})
			if tt.archived {
				_, err := stores.Agents.Archive(ctx, []string{orchestrator.ID})
				require.NoError(t, err)
			}

			worker := NewLaunchAgentWorker(NewLaunchAgentWorkerParams{
				Schedules: stores.Schedules,
				Runs:      stores.Schedules,
				Running:   stores.Agents,
				Orchestrators: schedules.NewScheduleService(schedules.NewScheduleServiceParams{
					Schedules: stores.Schedules,
					Workflows: stores.Workflows,
					Agents:    stores.Agents,
				}),
				Mailer: mail.NewMailService(mail.NewMailServiceParams{
					MailReadWriter: stores.Mail,
					Agents:         stores.Agents,
					Workflows:      stores.Workflows,
					Config:         config.Mail{MaxExchange: config.DefaultMaxExchange},
				}),
			})

			job := &river.Job[LaunchAgentArgs]{
				JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 5},
				Args:   LaunchAgentArgs{ScheduleID: schedule.ID, ScheduledFor: nine},
			}
			if tt.twice {
				require.NoError(t, worker.Work(ctx, job))
				job.Attempt = 2
			}
			err := worker.Work(ctx, job)
			if tt.wantReason != "" {
				require.Error(t, err)
				assert.True(t, isCancel(err), "a firing with nobody to mail cancels rather than being retried")
				assert.Contains(t, err.Error(), tt.wantReason)
			} else {
				require.NoError(t, err)
			}

			runs, err := stores.Schedules.ListRuns(ctx, schedule.ID, 10)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRuns, xslice.Map(runs, func(run schedulestore.Run) schedulestore.RunState {
				return run.State
			}))
			if tt.wantReason != "" {
				assert.Contains(t, runs[0].Message, tt.wantReason, "the run on the schedule says why it was skipped")
			}

			assert.Empty(t, listScheduleAgents(t, stores, schedule.ID), "an orchestrator schedule launches no agent")

			waiting, err := stores.Mail.Claim(ctx, orchestrator.ID, 10)
			require.NoError(t, err)
			if !tt.wantMailed {
				assert.Empty(t, waiting)

				return
			}

			require.Len(t, waiting, 1)
			assert.Equal(t, "check CI", waiting[0].Body)
			assert.Equal(t, "schedule morning-ci", waiting[0].FromName)
			assert.Nil(t, waiting[0].FromAgentID)
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
		Usage:              config.Usage{MaxAgents: params.maxAgents},
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
