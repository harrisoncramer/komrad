package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type runLogWriter struct {
	root string
}

// TestLogs covers the log keys paging an agent's logs into the pane under the stage.
func TestLogs(t *testing.T) {
	t.Parallel()

	t.Run("the logs key walks the run log, the agent log, and closed", func(t *testing.T) {
		t.Parallel()

		harness, runs := openLogs(t)
		newest := runs.write(t, 1, config.TriggerProvision)
		harness.fireTimers()

		harness.press("l")
		require.Len(t, harness.panel.ReadRespawns(), 1)
		assert.Contains(t, harness.panel.ReadRespawns()[0], newest)
		assert.Equal(t, "%9", harness.panel.ReadGuestPane(), "the agent's own pane is left where it is")

		harness.press("l")
		require.Len(t, harness.panel.ReadRespawns(), 2)
		assert.Contains(t, harness.panel.ReadRespawns()[1], "export WEAVE_AGENT_ID='a1'")

		harness.press("l")
		assert.NotEmpty(t, harness.panel.ReadKilledPanes(), "the log's own pane goes with it")

		harness.press("l")
		require.Len(t, harness.panel.ReadRespawns(), 3, "cycling past closed reopens at the first stop")
		assert.Contains(t, harness.panel.ReadRespawns()[2], newest)
	})

	t.Run("the older-run key steps back through the runs and wraps", func(t *testing.T) {
		t.Parallel()

		harness, runs := openLogs(t)
		first := runs.write(t, 1, config.TriggerProvision)
		second := runs.write(t, 2, config.TriggerResume)

		harness.press("l", "L", "L")

		assert.Equal(t, []string{second, first, second}, readPagedLogs(t, harness, first, second))
	})

	t.Run("the older-run key has nowhere to step with one run and opens nothing alone", func(t *testing.T) {
		t.Parallel()

		harness, runs := openLogs(t)
		runs.write(t, 1, config.TriggerProvision)

		harness.press("L")
		assert.Empty(t, harness.panel.ReadRespawns())

		harness.press("l", "L")
		assert.Len(t, harness.panel.ReadRespawns(), 1)
	})

	t.Run("the older-run key names which run is on screen", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			runs  int
			press []string
			want  string
		}{
			{
				name: "a single run is not worth counting",
				runs: 1,
				want: "older run",
			},
			{
				name: "the newest of three",
				runs: 3,
				want: "run 3/3",
			},
			{
				name:  "one step back",
				runs:  3,
				press: []string{"L"},
				want:  "run 2/3",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness, runs := openLogs(t)
				for number := 1; number <= tt.runs; number++ {
					runs.write(t, number, config.TriggerProvision)
				}

				harness.press(append(append([]string{"l"}, tt.press...), "?")...)

				assert.Contains(t, strings.Join(strings.Fields(readLegend(harness.readFrame())), " "), "L "+tt.want)
			})
		}
	})

	t.Run("the run log opens on the run that failed rather than a newer one", func(t *testing.T) {
		t.Parallel()

		failed := newRunning()
		failed.FailedRun = true
		failed.FailedRunLog = proto.Int32(1)
		harness, runs := openLogs(t, failed)
		first := runs.write(t, 1, config.TriggerProvision)
		runs.write(t, 2, config.TriggerResume)

		harness.press("l")
		harness.fireTimers()

		respawns := harness.panel.ReadRespawns()
		require.NotEmpty(t, respawns)
		assert.Contains(t, respawns[len(respawns)-1], first)
	})

	t.Run("a failed run nothing on disk carries opens the newest", func(t *testing.T) {
		t.Parallel()

		failed := newRunning()
		failed.FailedRun = true
		failed.FailedRunLog = proto.Int32(9)
		harness, runs := openLogs(t, failed)
		runs.write(t, 1, config.TriggerProvision)
		newest := runs.write(t, 2, config.TriggerResume)

		harness.press("l")

		require.Len(t, harness.panel.ReadRespawns(), 1)
		assert.Contains(t, harness.panel.ReadRespawns()[0], newest)
	})

	t.Run("the keys open nothing with no agent under the cursor", func(t *testing.T) {
		t.Parallel()

		harness, _ := openLogs(t)

		harness.press("k", "l")

		assert.Empty(t, harness.panel.ReadRespawns())
	})

	t.Run("the agent log follows the cursor onto the next agent", func(t *testing.T) {
		t.Parallel()

		second := newAgent("a2", "zeta", "w1")
		second.PaneId = "%8"
		second.Alive = proto.Bool(true)
		harness, _ := openLogs(t, newRunning(), second)

		harness.press("l", "l", "k")
		harness.fireTimers()

		respawns := harness.panel.ReadRespawns()
		require.Len(t, respawns, 3)
		assert.Contains(t, respawns[1], "export WEAVE_AGENT_ID='a1'")
		assert.Contains(t, respawns[2], "export WEAVE_AGENT_ID='a2'")
	})

	t.Run("the key is typed into an open search", func(t *testing.T) {
		t.Parallel()

		harness, _ := openLogs(t)

		harness.press("s", "l")

		assert.Contains(t, harness.readFrame(), "/ l")
		assert.Empty(t, harness.panel.ReadRespawns())
	})

	t.Run("the archive answers none of the pane keys", func(t *testing.T) {
		t.Parallel()

		harness, runs := openLogs(t)
		runs.write(t, 1, config.TriggerDeprovision)

		harness.press("Z", "l", "L", "T")

		assert.Empty(t, harness.panel.ReadRespawns())
	})

	tests := []struct {
		name  string
		press string
	}{
		{
			name:  "entering the archive closes the log pane",
			press: "Z",
		},
		{
			name:  "entering the search closes the log pane",
			press: "s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, _ := openLogs(t)
			harness.press("l", "l")
			require.Empty(t, harness.panel.ReadKilledPanes())

			harness.press(tt.press)

			assert.NotEmpty(t, harness.panel.ReadKilledPanes())
		})
	}
}

// TestLogsFailedPlaceholder covers what a failed agent's stage says about why it failed.
func TestLogsFailedPlaceholder(t *testing.T) {
	tests := []struct {
		name       string
		state      agentv1.AgentState
		reason     string
		runs       []string
		emptyRuns  bool
		want       string
		wantAbsent string
	}{
		{
			name:       "a script that left a log is pointed at rather than repeated",
			state:      agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			reason:     "failed to open a window: tmux refused",
			runs:       []string{config.TriggerProvision},
			want:       `Press "l"`,
			wantAbsent: "tmux refused",
		},
		{
			name:   "with no script run the reason is said inline",
			state:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			reason: "failed to open a window: tmux refused",
			want:   "tmux refused",
		},
		{
			name:      "a run with nothing in it is passed over",
			state:     agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			reason:    "tmux refused the window",
			runs:      []string{config.TriggerProvision},
			emptyRuns: true,
			want:      "tmux refused",
		},
		{
			name:   "a hook that failed the create is the run pointed at",
			state:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			reason: "install-deps: mise refused",
			runs:   []string{config.TriggerProvision, config.HookPreLaunch},
			want:   "run 002",
		},
		{
			name:       "a hook that failed the delete is the run pointed at",
			state:      agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP,
			reason:     "take-the-stack-down: docker refused",
			runs:       []string{config.TriggerProvision, config.HookPreTeardown},
			want:       "run 002",
			wantAbsent: "docker refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := newAgent("a1", "old", "w1")
			failed.State = tt.state
			failed.Metadata = []*agentv1.AgentMetadata{{Key: "error", Value: tt.reason}}
			harness, runs := openLogs(t)
			for i, trigger := range tt.runs {
				path := runs.write(t, i+1, trigger)
				if tt.emptyRuns {
					require.NoError(t, os.Truncate(path, 0))
				}
			}

			harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), failed))

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.want)
			if tt.wantAbsent != "" {
				assert.NotContains(t, frame, tt.wantAbsent)
			}
		})
	}
}

func openLogs(t *testing.T, agents ...*agentv1.Agent) (*panelHarness, runLogWriter) {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), "weave.log")
	require.NoError(t, os.WriteFile(logPath, []byte("listening on 127.0.0.1:8787\n"), 0o644))
	runs := runLogWriter{root: t.TempDir()}

	if len(agents) == 0 {
		agents = []*agentv1.Agent{newRunning()}
	}

	harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{LogPath: logPath, RunLogsDir: runs.root}})
	answerWorktreeReads(harness)
	rows := []proto.Message{newLiveWorkflow("w1", "api")}
	for _, agent := range agents {
		rows = append(rows, agent)
	}
	harness.watch(buildSnapshot(rows...))
	harness.press("j")

	return harness, runs
}

func (w runLogWriter) write(t *testing.T, number int, trigger string) string {
	t.Helper()

	dir := config.RunLogDir(w.root, "a1")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	path := filepath.Join(dir, config.RunLogName(number, trigger, "make-worktree"))
	require.NoError(t, os.WriteFile(path, []byte("cloning worktree\n"), 0o644))

	return path
}

func readPagedLogs(t *testing.T, harness *panelHarness, paths ...string) []string {
	t.Helper()

	var paged []string
	for _, respawn := range harness.panel.ReadRespawns() {
		for _, path := range paths {
			if strings.Contains(respawn, path) {
				paged = append(paged, path)
			}
		}
	}

	return paged
}
