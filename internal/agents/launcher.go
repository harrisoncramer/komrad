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
	tmux        paneWriter
	serverAddr  string
	cacheDir    string
	logPath     string
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

func (l *Launcher) appendedSystemPrompt(params launchParams) string {
	parts := []string{l.buildBriefing(), relationBriefing(params), l.globalContext(), params.WorkflowContext}

	trimmed := xslice.Map(parts, strings.TrimSpace)
	kept := xslice.Filter(trimmed, func(s string) bool { return s != "" })

	return strings.Join(kept, "\n\n")
}

// relationBriefing is what an agent is told about whoever it answers to, which for a child is
// the rules it runs under as well, since those are the reason it was made this way rather than
// as an agent of its own.
func relationBriefing(params launchParams) string {
	if params.ParentAgentID != "" {
		return childBriefing(params.ParentName, params.ParentAgentID)
	}

	return delegateBriefing(params.SpawnedByAgentID, params.SpawnedByName)
}

func delegateBriefing(spawnedByAgentID, spawnedByName string) string {
	if spawnedByAgentID == "" {
		return ""
	}

	return fmt.Sprintf(
		"You were spawned by agent %s (%s) to do this piece of work. Answer its mail as it arrives, and tell it when you are done rather than waiting to be asked.",
		spawnedByName, spawnedByAgentID)
}

func childBriefing(parentName, parentAgentID string) string {
	return fmt.Sprintf(`You are a child of agent %s (%s), attached to it rather than standing on your own. Answer its mail as it arrives, and tell it when you are done rather than waiting to be asked.

You are sitting in that agent's own checkout, which you own nothing in. Read it, run it, and say what you found, but do not commit, push, stash, or switch branches: the parent is working there and its next act is to keep working where you are reading. Put back anything you changed to try something out.

You have no worktree of your own and no scripts of your own, and you go when the parent goes, though the parent may also finish with you on your own once you have answered it. Mail reaches your parent and that parent's other children, and nobody else, so reply to your parent rather than looking for another way around. Your parent will name your siblings if there are any you need.`,
		parentName, parentAgentID)
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
	Pane             string
	Dir              string
	Flags            string
	Prompt           string
	WorkflowContext  string
	SessionID        string
	AgentID          string
	SpawnedByAgentID string
	SpawnedByName    string
	ParentAgentID    string
	ParentName       string
	Resume           bool
}

func (l *Launcher) launch(h harness.Harness, params launchParams) error {
	command, err := h.BuildCommand(harness.Launch{
		Flags:       params.Flags,
		Prompt:      params.Prompt,
		Dir:         params.Dir,
		Context:     l.appendedSystemPrompt(params),
		SessionID:   params.SessionID,
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
