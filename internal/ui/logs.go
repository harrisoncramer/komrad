package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

const logEnvPath = "WEAVE_LOG"

const agentIDEnvVar = "WEAVE_AGENT_ID"

func viewerArgv(viewer, fallback, path, agentID string) []string {
	command := strings.TrimSpace(viewer)
	if command == "" {
		command = fallback
	}

	exports := fmt.Sprintf("export %s=%s; ", logEnvPath, shellQuote(path))
	if agentID != "" {
		exports += fmt.Sprintf("export %s=%s; ", agentIDEnvVar, shellQuote(agentID))
	}

	return []string{"sh", "-c", exports + command}
}

func logArgv(viewer, path, agentID string) []string {
	return viewerArgv(viewer, config.DefaultLogViewer, path, agentID)
}

func processLogArgv(viewer, path string) []string {
	return viewerArgv(viewer, config.DefaultProcessViewer, path, "")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

var logCycle = []infoKind{infoSetupLog, infoTeardownLog, infoAgentLog}

func (m model) logsDesc() string {
	switch m.info.kind {
	case infoSetupLog:
		return "teardown log"
	case infoTeardownLog:
		return "agent log"
	case infoAgentLog:
		return "hide logs"
	default:
		return "setup log"
	}
}

// logTarget is the agent the log keys read, which follows the archive's own cursor while that
// screen is up rather than the agents list.
func (m model) logTarget() (agentstore.Agent, bool) {
	if m.screen == screenArchive {
		return m.selectedArchived()
	}

	item, ok := m.selectedAgent()
	if !ok {
		return agentstore.Agent{}, false
	}

	return item.agent, true
}

func (m model) cycleLogs() (tea.Model, tea.Cmd) {
	if m.setupPending {
		return m, nil
	}

	if _, ok := m.logTarget(); !ok {
		return m, nil
	}

	idx := slices.Index(logCycle, m.info.kind)
	if idx == len(logCycle)-1 {
		return m.closeInfo(), nil
	}

	if !m.claimInfo(logCycle[idx+1], "") {
		return m, nil
	}
	m.stageInfo()

	return m, nil
}

func (m model) actionLogContent(kind string) layout.Content {
	agent, ok := m.logTarget()
	if !ok {
		return newTextContent("select an agent to see its " + kind + " log")
	}

	if !m.hasActionLog(agent.ID, kind) {
		return newTextContent("no " + kind + " log yet")
	}

	path := config.ActionLogPath(m.actionLogsDir, agent.ID, kind)

	return newLogContent(processLogArgv(m.processViewer, path))
}

// hasActionLog reports whether a setup or teardown action left anything to read for an agent.
func (m model) hasActionLog(agentID, kind string) bool {
	if m.actionLogsDir == "" {
		return false
	}

	info, err := os.Stat(config.ActionLogPath(m.actionLogsDir, agentID, kind))

	return err == nil && info.Size() > 0
}

func (m model) agentLogContent() layout.Content {
	agent, ok := m.logTarget()
	if !ok {
		return newTextContent("select an agent to see its log")
	}

	return newLogContent(logArgv(m.logViewer, m.logPath, agent.ID))
}
