package internaljobs

import (
	"fmt"
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
	"weave/internal/tmux"
	"weave/lib/xslice"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScheduledLaunchCreatesOneAgent covers the store's side of the guarantee, where the
// idempotency key is all that stops a River retry making a second agent.
func TestScheduledLaunchCreatesOneAgent(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

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
			name:        "and a later firing launches once the last one is no longer working",
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
				Launcher:  newAgentService(t, stores),
				Running:   stores.Agents,
			})

			for i, occurrence := range tt.occurrences {
				err := worker.Work(t.Context(), &river.Job[LaunchAgentArgs]{
					JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 5},
					Args:   LaunchAgentArgs{ScheduleID: schedule.ID, ScheduledFor: occurrence},
				})

				last := i == len(tt.occurrences)-1
				if last && tt.wantSkipped {
					require.Error(t, err)
					assert.True(t, isCancel(err), "a firing that declines to launch cancels rather than failing")
				} else {
					require.NoError(t, err)
				}

				if i == 0 && tt.promoteAfterFirst {
					promoteScheduleAgents(t, stores, schedule.ID)
				}
			}

			listed, err := stores.Agents.List(t.Context())
			require.NoError(t, err)

			assert.Len(t, listed, tt.wantAgents)

			runs, err := stores.Schedules.ListRuns(t.Context(), schedule.ID, 10)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRuns, xslice.Map(runs, func(run schedulestore.Run) schedulestore.RunState { return run.State }))
		})
	}
}

const claudeHarnessID = "claude"

func newAgentService(t *testing.T, stores *storetest.Stores) *agents.LaunchService {
	t.Helper()

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))
	require.NoError(t, err)

	return agents.NewLaunchService(agents.NewLaunchServiceParams{
		Tmux:               &fakeTmux{},
		Agents:             stores.Agents,
		Reviews:            reviewstore.NewReviewStore(stores.Pool.GetDB()),
		Workflows:          stores.Workflows,
		Tx:                 stores.Pool,
		Launcher:           agents.NewLauncher(agents.NewLauncherParams{Tmux: &fakeTmux{}, Addr: "127.0.0.1:8787"}),
		Harnesses:          registry,
		UserDefaultHarness: claudeHarnessID,
	})
}

func promoteScheduleAgents(t *testing.T, stores *storetest.Stores, scheduleID string) {
	t.Helper()

	listed, err := stores.Agents.List(t.Context())
	require.NoError(t, err)

	for _, agent := range listed {
		if agent.ScheduleID == nil || *agent.ScheduleID != scheduleID {
			continue
		}

		require.NoError(t, stores.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateThinking))
	}
}

type fakeTmux struct {
	panes int
}

func (s *fakeTmux) NewWindow(_, name, _ string) (string, error) {
	s.panes++

	return fmt.Sprintf("%%%d-%s", s.panes, name), nil
}

func (s *fakeTmux) KillPane(string) {}

func (s *fakeTmux) SetPaneOwner(string, string) error { return nil }

func (s *fakeTmux) GetPaneOwner(string) string { return "" }

func (s *fakeTmux) GetPanes() (map[string]tmux.PaneStatus, error) {
	return map[string]tmux.PaneStatus{}, nil
}

func (s *fakeTmux) GetHostPath() string { return "" }

func (s *fakeTmux) SendKeys(string, ...string) error { return nil }

func (s *fakeTmux) RespawnPane(string, string, ...string) error { return nil }

func (s *fakeTmux) GetDefaultShell() string { return "/bin/sh" }
