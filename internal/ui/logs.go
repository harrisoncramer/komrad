package ui

import (
	"fmt"
	"os"
	"strings"

	"weave/internal/appconfig"

	tea "github.com/charmbracelet/bubbletea"
)

// The service's log, read from the panel. The service is spawned detached the first time
// the popup opens and writes to a file rather than to a terminal, so the panel is the only
// place somebody would think to look for it.

// logEnv is what a viewer command reads its inputs from. They are exported into the
// command's environment rather than substituted into its text, which is what keeps the
// panel out of the way of the tools people put here: fzf's own "{}" means the selected
// line, and a panel that rewrote every "{}" before fzf saw it would make half the recipes
// in fzf's own manual impossible to write.
const (
	logEnvPath   = "WEAVE_LOG"
	logEnvBinary = "WEAVE_BIN"
)

// logArgv is what the stage runs to show the log. The configured command goes through a
// shell rather than being split into arguments, because the readers worth having here are
// pipelines, and "weave logs | fzf" is not something an argv can express.
func logArgv(viewer, path, binary string) []string {
	command := strings.TrimSpace(viewer)
	if command == "" {
		command = appconfig.DefaultLogViewer
	}

	exports := fmt.Sprintf("export %s=%s %s=%s; ",
		logEnvPath, shellQuote(path), logEnvBinary, shellQuote(binary))

	return []string{"sh", "-c", exports + command}
}

// shellQuote wraps a value so the shell reads it as one word whatever is in it. Single
// quotes take everything literally, and the only thing they cannot hold is a single quote,
// which is closed and reopened around.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// logsDesc is what the legend says the log key would do, naming the direction it would go
// rather than the binding in the abstract, the way the info and hide lines read.
func (m model) logsDesc() string {
	if m.tree.logsPinned {
		return "hide server log"
	}

	return "server log"
}

// openLogs shows the service's log in the stage in place of whatever the cursor was on,
// and pressing the key again puts the row back. It is pinned the way info is rather than
// taking the screen, so the list stays beside it and moving the cursor is the other way
// out.
//
// The log is read off disk rather than asked for over RPC, since it is opened when the
// service is failing, which is when a call to that service is least likely to answer.
//
// A log that cannot be read is bannered rather than pinned. less given a path it cannot
// open prints its complaint and exits, which takes the stage pane down with it and leaves
// the key looking like it did nothing.
func (m model) openLogs() (tea.Model, tea.Cmd) {
	if !m.tree.logsPinned {
		if _, err := os.Stat(m.logPath); err != nil {
			m.report("Could not read the server log", err)

			return m, nil
		}
	}

	m.tree.logsPinned = !m.tree.logsPinned
	m.tree.infoPinned = false
	m.stageSelection()

	return m, nil
}
