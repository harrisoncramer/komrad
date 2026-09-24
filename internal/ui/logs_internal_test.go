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

// TestLogArgv covers the shell command a log viewer is run as.
func TestLogArgv(t *testing.T) {
	tests := []struct {
		name        string
		argv        []string
		wantViewer  string
		wantExports []string
		wantAbsent  string
	}{
		{
			name:        "nothing configured falls back to a pager on the file",
			argv:        logArgv("", "/tmp/weave.log", ""),
			wantViewer:  config.DefaultLogViewer,
			wantExports: []string{"export WEAVE_LOG='/tmp/weave.log'; "},
			wantAbsent:  "export WEAVE_AGENT_ID",
		},
		{
			name:        "a viewer that is only whitespace is no viewer at all",
			argv:        logArgv("   ", "/tmp/weave.log", ""),
			wantViewer:  config.DefaultLogViewer,
			wantExports: []string{"export WEAVE_LOG='/tmp/weave.log'; "},
		},
		{
			name:       "a pipeline is left exactly as it was written",
			argv:       logArgv(`jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`, "/tmp/weave.log", "a1"),
			wantViewer: `jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`,
			wantExports: []string{
				"export WEAVE_LOG='/tmp/weave.log'; ",
				"export WEAVE_AGENT_ID='a1'; ",
			},
		},
		{
			name:        "a path holding spaces and quotes survives the export",
			argv:        logArgv("", "/Users/a b/it's.log", ""),
			wantViewer:  config.DefaultLogViewer,
			wantExports: []string{`export WEAVE_LOG='/Users/a b/it'\''s.log'`},
		},
		{
			name:        "a process log falls back to its own viewer and names no agent",
			argv:        processLogArgv("", "/tmp/setup.log"),
			wantViewer:  config.DefaultProcessViewer,
			wantExports: []string{"export WEAVE_LOG='/tmp/setup.log'; "},
			wantAbsent:  "WEAVE_AGENT_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.argv, 3)
			assert.Equal(t, []string{"sh", "-c"}, tt.argv[:2], "a pipeline needs a shell to read it")
			assert.Contains(t, tt.argv[2], tt.wantViewer)
			for _, export := range tt.wantExports {
				assert.Contains(t, tt.argv[2], export)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, tt.argv[2], tt.wantAbsent)
			}
		})
	}
}

// TestLogPlaceholders covers what a log pane says when there is no log to page.
func TestLogPlaceholders(t *testing.T) {
	m, _ := failedRunTree()
	m.settings.RunLogsDir = t.TempDir()

	m.agents.cursor = 0
	assert.Contains(t, m.runLogContent().Key, "select an agent")
	assert.Contains(t, m.agentLogContent().Key, "select an agent")

	m.agents.cursor = 1
	assert.Contains(t, m.runLogContent().Key, "no script has run")
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
			m.agents.cursor = tt.cursor
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

// TestTheRunLogFollowsTheCursorOntoTheFailedRun covers the run log moving to the failed run when the cursor reaches it.
func TestTheRunLogFollowsTheCursorOntoTheFailedRun(t *testing.T) {
	at := time.Now()

	logsDir := t.TempDir()
	seedRunLogs(t, logsDir, "a1", 2)
	seedRunLogs(t, logsDir, "a2", 2)

	m, _ := failedRunTree()
	m.settings.RunLogsDir = logsDir
	m.listing.agents[0].FailedRunLog = xptr.Ptr(int32(1))
	m.info.kind = infoRunLog

	m.agents.cursor = 2
	m, _ = m.dwellRunsRead(at)
	require.Equal(t, 0, m.info.run)

	m.agents.cursor = 1
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

// TestRunsReadRestartsWhenTheCursorMoves covers the runs-read hold restarting when the cursor leaves the row.
func TestRunsReadRestartsWhenTheCursorMoves(t *testing.T) {
	at := time.Now()

	m, _ := failedRunTree()
	m.info.kind = infoRunLog
	m.agents.cursor = 1

	m, _ = m.dwellRunsRead(at)

	m.agents.cursor = 2
	m, _ = m.dwellRunsRead(at.Add(viewedDwell / 2))
	m.agents.cursor = 1

	m, cmd := m.dwellRunsRead(at.Add(viewedDwell))
	assert.Nil(t, cmd, "the pane only just came back to the row that failed")

	_, cmd = m.dwellRunsRead(at.Add(viewedDwell).Add(viewedDwell))
	assert.NotNil(t, cmd)
}

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
			agents: []panel.AgentRow{
				{
					Agent: panel.Agent{
						ID:         "a1",
						Name:       "auth",
						WorkflowID: "w1",
					},
					Alive:     true,
					FailedRun: true,
				},
				{
					Agent: panel.Agent{
						ID:         "a2",
						Name:       "api",
						WorkflowID: "w1",
					},
					Alive: true,
				},
			},
		},
	}, tmux
}
