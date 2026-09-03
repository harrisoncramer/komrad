package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
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
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "auth",
						WorkflowID: "w1",
					},
					alive:     true,
					failedRun: true,
				},
				{
					agent: agentstore.Agent{
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

// TestFailedRunMarkIsDrawnOnTheRow covers the point of the whole change: a hook that failed
// after the agent came up is visible without opening anything.
func TestFailedRunMarkIsDrawnOnTheRow(t *testing.T) {
	m, _ := failedRunTree()

	rows := strings.Split(m.agentsView(), "\n")

	var carrying, clear string
	for _, row := range rows {
		switch {
		case strings.Contains(row, "auth"):
			carrying = row
		case strings.Contains(row, "api"):
			clear = row
		}
	}

	require.NotEmpty(t, carrying)
	require.NotEmpty(t, clear)
	assert.Contains(t, carrying, failedRunIcon)
	assert.NotContains(t, clear, failedRunIcon)
}

// TestRunsReadNeedsTheRunLogUpAndReadForASecond pins what counts as reading a failed run, since
// a mark cleared by a cursor passing over a row, or while nobody is looking, is worth nothing.
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
// is about, since an agent resumed since its hook failed has a newer run that went fine and the
// pane would otherwise open on that one and clear the mark.
func TestOpeningTheRunLogLandsOnTheRunThatFailed(t *testing.T) {
	logsDir := t.TempDir()
	logDir := config.RunLogDir(logsDir, "a1")
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	for _, run := range []struct {
		number  int
		trigger string
	}{
		{number: 1, trigger: config.HookPostLaunch},
		{number: 2, trigger: config.TriggerResume},
	} {
		name := config.RunLogName(run.number, run.trigger, "setup")
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}

	m, _ := failedRunTree()
	m.runLogsDir = logsDir
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
