package agents

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"weave/internal/harness"
	"weave/lib/xslice"
)

//go:embed briefing.md
var briefingDoc string

const mcpURLPlaceholder = "{{MCP_URL}}"

const logPathPlaceholder = "{{LOG_PATH}}"

type Launcher struct {
	tmux paneWriter

	serverAddr string

	cacheDir string
	logPath  string

	contextPath string

	hookCommand string
}

type LauncherParams struct {
	Tmux        paneWriter
	Addr        string
	CacheDir    string
	LogPath     string
	ContextPath string
	HookCommand string
}

// NewLauncher builds a launcher that points spawned agents at the given address.
func NewLauncher(p LauncherParams) *Launcher {
	if p.Addr == "" {
		p.Addr = defaultServerAddr
	}

	return &Launcher{
		tmux:        p.Tmux,
		serverAddr:  p.Addr,
		cacheDir:    p.CacheDir,
		logPath:     p.LogPath,
		contextPath: p.ContextPath,
		hookCommand: p.HookCommand,
	}
}

func (l *Launcher) buildBriefing() string {
	text := strings.ReplaceAll(briefingDoc, mcpURLPlaceholder, fmt.Sprintf("http://%s/mcp", l.serverAddr))

	return strings.ReplaceAll(text, logPathPlaceholder, l.logPath)
}

func (l *Launcher) appendedSystemPrompt(workflowContext, spawnedByAgentID, spawnedByName string) string {
	parts := []string{l.buildBriefing(), delegateBriefing(spawnedByAgentID, spawnedByName), l.globalContext(), workflowContext}

	trimmed := xslice.Map(parts, strings.TrimSpace)
	kept := xslice.Filter(trimmed, func(s string) bool { return s != "" })

	return strings.Join(kept, "\n\n")
}

func delegateBriefing(spawnedByAgentID, spawnedByName string) string {
	if spawnedByAgentID == "" {
		return ""
	}

	return fmt.Sprintf(
		"You were spawned by agent %s (%s) to do this piece of work. Answer its mail as it arrives, and tell it when you are done rather than waiting to be asked.",
		spawnedByName, spawnedByAgentID)
}

func (l *Launcher) globalContext() string {
	if l.contextPath == "" {
		return ""
	}

	text, err := os.ReadFile(l.contextPath)
	if err != nil {
		return ""
	}

	return string(text)
}

type launchParams struct {
	Pane            string
	Dir             string
	Flags           string
	Prompt          string
	WorkflowContext string
	SessionID       string
	ResumeFrom      string
	AgentID         string

	SpawnedByAgentID string
	SpawnedByName    string
	Resume           bool
}

func (l *Launcher) launch(h harness.Harness, params launchParams) error {
	command, err := h.BuildCommand(harness.Launch{
		Flags:       params.Flags,
		Prompt:      params.Prompt,
		Dir:         params.Dir,
		Context:     l.appendedSystemPrompt(params.WorkflowContext, params.SpawnedByAgentID, params.SpawnedByName),
		SessionID:   params.SessionID,
		ResumeFrom:  params.ResumeFrom,
		AgentID:     params.AgentID,
		MCPEndpoint: l.serverAddr,
		CacheDir:    l.cacheDir,
		HookCommand: l.hookCommand,
		Resume:      params.Resume,
	})
	if err != nil {
		return fmt.Errorf("failed to build the launch command: %w", err)
	}

	if err := l.tmux.SendKeys(params.Pane, command, "Enter"); err != nil {
		return fmt.Errorf("failed to type the launch command: %w", err)
	}

	return nil
}
