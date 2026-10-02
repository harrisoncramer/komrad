package agents

import (
	"sync/atomic"
	"testing"
	"time"

	"komrad/internal/harnesstest"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentWatchesTheCLIStartUp pins a CLI that dies on launch being marked failed with what it printed.
func TestNewAgentWatchesTheCLIStartUp(t *testing.T) {
	type test struct {
		name       string
		exited     bool
		screen     string
		wantState  agentstore.State
		wantReason string
	}

	tests := []test{
		{
			name:       "a CLI that exits while starting",
			exited:     true,
			screen:     "\x1b[31mError: the argument '--dangerously-bypass-approvals-and-sandbox' cannot be used multiple times\x1b[0m\n\nuser@host ~ %\n",
			wantState:  agentstore.StateFailedToCreate,
			wantReason: "the CLI exited while starting: Error: the argument '--dangerously-bypass-approvals-and-sandbox' cannot be used multiple times user@host ~ %",
		},
		{
			name:      "a CLI that keeps running",
			exited:    false,
			screen:    "ready\n",
			wantState: agentstore.StateIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			tmux.Exited["%agent"] = tt.exited
			tmux.Captures["%agent"] = tt.screen
			st := newStore(t)

			var polls atomic.Int32
			manager := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
				addr:      "127.0.0.1:9999",
				lifecycle: &fakeLifecycle{},
				wait: func(time.Duration) <-chan time.Time {
					polls.Add(1)
					rested := make(chan time.Time, 1)
					rested <- time.Now()

					return rested
				},
			})

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name: "backend",
				Cwd:  t.TempDir(),
			})
			require.NoError(t, err)

			_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			assert.Eventually(t, func() bool {
				agents, err := st.Agents.List(t.Context())
				if err != nil || len(agents) != 1 {
					return false
				}

				return agents[0].State == tt.wantState && (tt.exited || polls.Load() == startupPolls)
			}, 5*time.Second, 10*time.Millisecond)

			agents, err := st.Agents.List(t.Context())
			require.NoError(t, err)
			require.Len(t, agents, 1)
			assert.Equal(t, tt.wantReason, agents[0].FailureReason)
		})
	}
}
