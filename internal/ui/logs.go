package ui

import (
	"fmt"
	"os"
	"strings"

	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

const logEnvPath = "WEAVE_LOG"

func logArgv(viewer, path string) []string {
	command := strings.TrimSpace(viewer)
	if command == "" {
		command = config.DefaultLogViewer
	}

	exports := fmt.Sprintf("export %s=%s; ", logEnvPath, shellQuote(path))

	return []string{"sh", "-c", exports + command}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func (m model) logsDesc() string {
	if m.info.kind == infoLogs {
		return "hide server log"
	}

	return "server log"
}

func (m model) openLogs() (tea.Model, tea.Cmd) {
	if m.info.kind == infoLogs {
		return m.closeInfo(), nil
	}

	if _, err := os.Stat(m.logPath); err != nil {
		m.report("Could not read the server log", err)

		return m, nil
	}

	m.info.kind = infoLogs
	m.info.action = ""
	m.stageInfo()

	return m, nil
}
