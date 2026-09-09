package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"
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

var logCycle = []infoKind{infoRunLog, infoAgentLog}

func (m model) logsDesc() string {
	switch m.info.kind {
	case infoRunLog:
		return "agent log"
	case infoAgentLog:
		return "hide logs"
	case infoNothing, infoTerminal, infoEditFile:
	}

	return "run log"
}

// logTarget is the agent the log keys read.
func (m model) logTarget() (agentstore.Agent, bool) {
	item, ok := m.selectedAgent()
	if !ok {
		return agentstore.Agent{}, false
	}

	return item.agent, true
}

func (m model) cycleLogs() model {
	agent, ok := m.logTarget()
	if !ok {
		return m
	}

	idx := slices.Index(logCycle, m.info.kind)
	if idx == len(logCycle)-1 {
		return m.closeInfo()
	}

	if !m.claimInfo(logCycle[idx+1], "") {
		return m
	}

	// A mark says there is a failed run to read, so the pane opens on that run rather than on
	// whatever ran last.
	if m.info.kind == infoRunLog {
		m.info.run = m.runAt(agent.ID, m.failedRunLog(agent.ID))
	}
	m.stageInfo()

	return m
}

// olderLog steps the info pane back to the run before the one it is showing, wrapping round to
// the most recent.
func (m model) olderLog() model {
	if m.info.kind != infoRunLog {
		return m
	}

	agent, ok := m.logTarget()
	if !ok {
		return m
	}

	runs := config.ListRunLogs(m.runLogsDir, agent.ID)
	if len(runs) < 2 {
		return m
	}

	m.info.run = (m.info.run + 1) % len(runs)
	m.stageInfo()

	return m
}

// runPosition labels the older-run key with where through an agent's runs the pane has got,
// counting from the newest.
func (m model) runPosition() string {
	agent, ok := m.logTarget()
	if !ok {
		return "older run"
	}

	total := len(config.ListRunLogs(m.runLogsDir, agent.ID))
	if total < 2 {
		return "older run"
	}

	return fmt.Sprintf("run %d/%d", total-min(m.info.run, total-1), total)
}

func (m model) runLogContent() layout.Content {
	agent, ok := m.logTarget()
	if !ok {
		return newTextContent("select an agent to see its run log")
	}

	run, ok := m.runLog(agent.ID, m.info.run)
	if !ok {
		return newTextContent("no script has run for this agent yet")
	}

	return newLogContent(processLogArgv(m.processViewer, run.Path))
}

// runLog is the run the pane is showing, counting back from the most recent, falling back to
// the most recent one there when the one it was showing has since been pruned.
func (m model) runLog(agentID string, back int) (config.RunLog, bool) {
	runs := config.ListRunLogs(m.runLogsDir, agentID)
	if len(runs) == 0 {
		return config.RunLog{}, false
	}

	if back >= len(runs) {
		back = 0
	}

	return runs[back], true
}

// runAt is how far back through an agent's runs a log number sits, zero being the most recent
// and zero again when nothing on disk carries that number.
func (m model) runAt(agentID string, number *int32) int {
	if number == nil {
		return 0
	}

	for at, run := range config.ListRunLogs(m.runLogsDir, agentID) {
		if run.Number == int(*number) {
			return at
		}
	}

	return 0
}

// failedRunLog is the run log an agent's unread failure was written to, and nothing for a row
// carrying no mark.
func (m model) failedRunLog(agentID string) *int32 {
	for _, item := range m.listing.agents {
		if item.agent.ID == agentID {
			return item.failedRunLog
		}
	}

	return nil
}

// latestRunLog is the most recent run at one of the given moments that has anything to read.
func (m model) latestRunLog(agentID string, moments []string) (config.RunLog, bool) {
	for _, run := range config.ListRunLogs(m.runLogsDir, agentID) {
		if !slices.Contains(moments, run.Trigger) {
			continue
		}

		if info, err := os.Stat(run.Path); err != nil || info.Size() == 0 {
			continue
		}

		return run, true
	}

	return config.RunLog{}, false
}

func (m model) agentLogContent() layout.Content {
	agent, ok := m.logTarget()
	if !ok {
		return newTextContent("select an agent to see its log")
	}

	return newLogContent(logArgv(m.logViewer, m.logPath, agent.ID))
}
