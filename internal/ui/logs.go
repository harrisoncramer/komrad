package ui

import (
	"fmt"
	"os"
	"strings"

	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

// logEnvPath is where a viewer command reads the file from. It is exported into the
// command's environment rather than substituted into its text, which is what keeps the
// panel out of the way of the tools people put here: fzf's own "{}" means the selected
// line, and a panel that rewrote every "{}" before fzf saw it would make half the recipes
// in fzf's own manual impossible to write.
const logEnvPath = "WEAVE_LOG"

// logArgv is what the stage runs to show the log. The configured command goes through a
// shell rather than being split into arguments, because the readers worth having here are
// pipelines, and "jq . $WEAVE_LOG | less" is not something an argv can express.
func logArgv(viewer, path string) []string {
	command := strings.TrimSpace(viewer)
	if command == "" {
		command = config.DefaultLogViewer
	}

	exports := fmt.Sprintf("export %s=%s; ", logEnvPath, shellQuote(path))

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
	if m.tree.below == belowLogs {
		return "hide server log"
	}

	return "server log"
}

// openLogs shows the service's log in the pane under the stage, and pressing the key again
// takes that pane away. It goes below rather than into the stage so the agent stays on
// screen while the log is read.
//
// The log is read off disk rather than asked for over RPC, since it is opened when the
// service is failing, which is when a call to that service is least likely to answer.
//
// A log that cannot be read is bannered rather than opened. less given a path it cannot
// open prints its complaint and exits, which takes the pane down with it and leaves the key
// looking like it did nothing.
func (m model) openLogs() (tea.Model, tea.Cmd) {
	if m.tree.below == belowLogs {
		return m.closeBelow(), nil
	}

	if _, err := os.Stat(m.logPath); err != nil {
		m.report("Could not read the server log", err)

		return m, nil
	}

	m.tree.below = belowLogs
	m.tree.belowAction = ""
	m.stageBelow()

	return m, nil
}
