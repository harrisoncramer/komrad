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

var logCycle = []infoKind{infoRunLog, infoAgentLog}

func (m model) logsDesc() string {
	switch m.info.kind {
	case infoRunLog:
		return "agent log"
	case infoAgentLog:
		return "hide logs"
	default:
		return "run log"
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

// olderLog steps the info pane back to the run before the one it is showing, wrapping round
// to the most recent, since one direction reaches every run and two keys for a handful of
// files is a key too many.
func (m model) olderLog() (tea.Model, tea.Cmd) {
	if m.info.kind != infoRunLog {
		return m, nil
	}

	agent, ok := m.logTarget()
	if !ok {
		return m, nil
	}

	runs := config.ListRunLogs(m.runLogsDir, agent.ID)
	if len(runs) < 2 {
		return m, nil
	}

	m.info.run = (m.info.run + 1) % len(runs)
	m.stageInfo()

	return m, nil
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

// latestRunLog is the most recent run at one of the given moments that has anything to read,
// since a run refused before its script wrote a byte points at an empty pager.
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
