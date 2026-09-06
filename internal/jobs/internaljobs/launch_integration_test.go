package internaljobs

import (
	"fmt"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/internal/tmux"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScheduledLaunchCreatesOneAgent covers the store's side of the guarantee, since River
// retries with the same arguments and only the idempotency key stops a second agent.
func TestScheduledLaunchCreatesOneAgent(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name              string
		occurrences       []time.Time
		promoteAfterFirst bool
		wantSkipped       bool
		wantAgents        int
	}{
		{
			name:              "a later firing skips while the last agent is still working",
			occurrences:       []time.Time{nine, nine.Add(24 * time.Hour)},
			promoteAfterFirst: true,
			wantSkipped:       true,
			wantAgents:        1,
		},
		{
			name:        "working the same firing twice launches one agent",
			occurrences: []time.Time{nine, nine},
			wantAgents:  1,
		},
		{
			name:        "and a later firing launches once the last one is no longer working",
			occurrences: []time.Time{nine, nine.Add(24 * time.Hour)},
			wantAgents:  2,
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

			worker := NewLaunchAgentWorker(stores.Schedules, newAgentService(t, stores), stores.Agents, stores.Schedules, nil)

			for i, occurrence := range tt.occurrences {
				err := worker.Work(t.Context(), &river.Job[LaunchAgentArgs]{
					Args: LaunchAgentArgs{ScheduleID: schedule.ID, ScheduledFor: occurrence},
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
		})
	}
}

const claudeHarnessID = "claude"

func newAgentService(t *testing.T, stores *storetest.Stores) *agents.LaunchService {
	t.Helper()

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()}))
	require.NoError(t, err)

	return agents.NewLaunchService(agents.LaunchServiceParams{
		Tmux:               &stubTmux{},
		Agents:             stores.Agents,
		Workflows:          stores.Workflows,
		Tx:                 stores.Pool,
		Launcher:           agents.NewLauncher(agents.LauncherParams{Tmux: &stubTmux{}, Addr: "127.0.0.1:8787"}),
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

type stubTmux struct {
	panes int
}

func (s *stubTmux) NewWindow(_, name, _ string) (string, error) {
	s.panes++

	return fmt.Sprintf("%%%d-%s", s.panes, name), nil
}

func (s *stubTmux) KillPane(string) {}

func (s *stubTmux) SetPaneOwner(string, string) error { return nil }

func (s *stubTmux) GetPaneOwner(string) string { return "" }

func (s *stubTmux) ClearPaneOwner(string) error { return nil }

func (s *stubTmux) GetPanes() (map[string]tmux.PaneStatus, error) {
	return map[string]tmux.PaneStatus{}, nil
}

func (s *stubTmux) GetHostPath() string { return "" }

func (s *stubTmux) SendKeys(string, ...string) error { return nil }
