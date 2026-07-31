package ui

import (
	"os"
	"strings"

	"weave/internal/appconfig"

	tea "github.com/charmbracelet/bubbletea"
)

// The service's log, read from the panel. The service is spawned detached the first time
// the popup opens and writes to a file rather than to a terminal, so the panel is the only
// place somebody would think to look for it.

// logPlaceholder is what the configured viewer writes where it wants the log's path. It is
// fzf's own placeholder, which is the tool most likely to be on the other side of a pipe
// here, and a log viewer is one of the few commands a person is likely to have written one
// of these for already.
const logPlaceholder = "{}"

// logArgv is what the stage runs to show the log. The configured command goes through a
// shell rather than being split into arguments, because the useful readers for a file of
// JSON records are pipelines, and "jq -C . {} | fzf" is not something an argv can express.
//
// The path replaces the placeholder, quoted, so a home directory with a space in it does
// not arrive as two arguments. A command that names no placeholder is handed the path at
// the end instead, which is what lets "less -R" or a bare pager name work as written.
func logArgv(viewer, path string) []string {
	command := strings.TrimSpace(viewer)
	if command == "" {
		command = appconfig.DefaultLogViewer
	}

	quoted := shellQuote(path)
	if strings.Contains(command, logPlaceholder) {
		command = strings.ReplaceAll(command, logPlaceholder, quoted)
	} else {
		command += " " + quoted
	}

	return []string{"sh", "-c", command}
}

// shellQuote wraps a path so the shell reads it as one word whatever is in it. Single
// quotes take everything literally, and the only thing they cannot hold is a single quote,
// which is closed and re-opened around.
func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
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
