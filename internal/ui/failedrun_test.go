package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func failedRunTree() (model, *recordingTmux) {
	tmux := &recordingTmux{sessionClient: panelViewer}

	return model{
		frame:  layout.New(tmux, config.DefaultLayout()),
		popup:  NewPopupWindow(tmux, "test-session"),
		keys:   config.DefaultKeybindings(),
		width:  80,
		screen: screenAgents,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{
					agent: panel.Agent{
						ID:         "a1",
						Name:       "auth",
						WorkflowID: "w1",
					},
					alive:     true,
					failedRun: true,
				},
				{
					agent: panel.Agent{
						ID:         "a2",
						Name:       "api",
						WorkflowID: "w1",
					},
					alive: true,
				},
			},
		},
	}, tmux
}

// TestRunsReadNeedsTheRunLogUpAndReadForASecond pins what counts as reading a failed run.
func TestRunsReadNeedsTheRunLogUpAndReadForASecond(t *testing.T) {
	tests := []struct {
		name    string
		kind    infoKind
		cursor  int
		hidden  bool
		wantCmd bool
	}{
		{
			name:   "nothing is open",
			kind:   infoNothing,
			cursor: 1,
		},
		{
			name:   "the agent's own log rather than its runs",
			kind:   infoAgentLog,
			cursor: 1,
		},
		{
			name:   "the run log, on a row carrying no mark",
			kind:   infoRunLog,
			cursor: 2,
		},
		{
			name:   "the run log, on the row that failed, with the panel hidden",
			kind:   infoRunLog,
			cursor: 1,
			hidden: true,
		},
		{
			name:    "the run log, on the row that failed, in front of somebody",
			kind:    infoRunLog,
			cursor:  1,
			wantCmd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := time.Now()

			m, tmux := failedRunTree()
			m.info.kind = tt.kind
			m.tree.cursor = tt.cursor
			if tt.hidden {
				tmux.sessionClient = ""
			}

			m, cmd := m.dwellRunsRead(at)
			assert.Nil(t, cmd, "the tick the pane arrived on claims nothing")

			_, cmd = m.dwellRunsRead(at.Add(viewedDwell))

			assert.Equal(t, tt.wantCmd, cmd != nil)
		})
	}
}

// TestOpeningTheRunLogLandsOnTheRunThatFailed covers the mark taking the person to the run it
// is about and not to a newer one that went fine.
func TestOpeningTheRunLogLandsOnTheRunThatFailed(t *testing.T) {
	logsDir := t.TempDir()
	seedRunLogs(t, logsDir, "a1", 2)

	m, _ := failedRunTree()
	m.settings.RunLogsDir = logsDir
	m.listing.agents[0].failedRunLog = xptr.Ptr(int32(1))
	m.tree.cursor = 1

	assert.Equal(t, 1, m.runAt("a1", xptr.Ptr(int32(1))), "the failed run is one back from the newest")
	assert.Equal(t, 0, m.runAt("a1", xptr.Ptr(int32(2))))
	assert.Equal(t, 0, m.runAt("a1", nil), "no mark opens on the newest run the way it always did")
	assert.Equal(t, 0, m.runAt("a1", xptr.Ptr(int32(9))), "and so does a number nothing on disk carries")

	require.NotNil(t, m.failedRunLog("a1"))
	assert.Equal(t, int32(1), *m.failedRunLog("a1"))
	assert.Nil(t, m.failedRunLog("a2"), "a row carrying no mark names no run")
}

// TestTheRunLogFollowsTheCursorOntoTheFailedRun covers the pane's target changing under a position
// worked out for the agent before it.
func TestTheRunLogFollowsTheCursorOntoTheFailedRun(t *testing.T) {
	at := time.Now()

	logsDir := t.TempDir()
	seedRunLogs(t, logsDir, "a1", 2)
	seedRunLogs(t, logsDir, "a2", 2)

	m, _ := failedRunTree()
	m.settings.RunLogsDir = logsDir
	m.listing.agents[0].failedRunLog = xptr.Ptr(int32(1))
	m.info.kind = infoRunLog

	// The pane was opened on the unmarked agent, so it sits on that agent's newest run.
	m.tree.cursor = 2
	m, _ = m.dwellRunsRead(at)
	require.Equal(t, 0, m.info.run)

	// The cursor reaches the agent that failed, whose newest run is the one that went fine.
	m.tree.cursor = 1
	m, cmd := m.dwellRunsRead(at.Add(time.Second))

	assert.Nil(t, cmd, "the tick it arrived on claims nothing")
	assert.Equal(t, 1, m.info.run, "the pane moved to the run the mark is about")

	_, cmd = m.dwellRunsRead(at.Add(time.Second).Add(viewedDwell))
	assert.NotNil(t, cmd, "and only then is the mark claimed")
}

// seedRunLogs writes an agent count numbered run logs, oldest first.
func seedRunLogs(t *testing.T, logsDir, agentID string, count int) {
	t.Helper()

	logDir := config.RunLogDir(logsDir, agentID)
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	for number := 1; number <= count; number++ {
		name := config.RunLogName(number, config.HookPostLaunch, "setup")
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}
}

// TestRunPositionNamesWhichRunIsOnScreen covers the label on the older-run key.
func TestRunPositionNamesWhichRunIsOnScreen(t *testing.T) {
	tests := []struct {
		name string
		runs int
		back int
		want string
	}{
		{
			name: "an agent that has run nothing",
			runs: 0,
			want: "older run",
		},
		{
			name: "a single run is not worth counting",
			runs: 1,
			want: "older run",
		},
		{
			name: "the newest of three",
			runs: 3,
			back: 0,
			want: "run 3/3",
		},
		{
			name: "one step back",
			runs: 3,
			back: 1,
			want: "run 2/3",
		},
		{
			name: "the oldest kept",
			runs: 3,
			back: 2,
			want: "run 1/3",
		},
		{
			name: "a position past what is left falls to the newest",
			runs: 3,
			back: 9,
			want: "run 1/3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logsDir := t.TempDir()
			if tt.runs > 0 {
				seedRunLogs(t, logsDir, "a1", tt.runs)
			}

			m, _ := failedRunTree()
			m.settings.RunLogsDir = logsDir
			m.tree.cursor = 1
			m.info.run = tt.back

			assert.Equal(t, tt.want, m.runPosition())
		})
	}
}

// TestRunsReadRestartsWhenTheCursorMoves pins the hold belonging to the row rather than to the
// pane, so walking the tree with the run log open never claims the rows travelled over.
func TestRunsReadRestartsWhenTheCursorMoves(t *testing.T) {
	at := time.Now()

	m, _ := failedRunTree()
	m.info.kind = infoRunLog
	m.tree.cursor = 1

	m, _ = m.dwellRunsRead(at)

	// The cursor lands on the unmarked row and comes back, which restarts the hold.
	m.tree.cursor = 2
	m, _ = m.dwellRunsRead(at.Add(viewedDwell / 2))
	m.tree.cursor = 1

	m, cmd := m.dwellRunsRead(at.Add(viewedDwell))
	assert.Nil(t, cmd, "the pane only just came back to the row that failed")

	_, cmd = m.dwellRunsRead(at.Add(viewedDwell).Add(viewedDwell))
	assert.NotNil(t, cmd)
}
