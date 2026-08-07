package agents

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"weave/internal/harness"
	"weave/lib/xslice"
)

// briefingDoc is what every spawned agent is told about the panel running it. It is prose in
// a file rather than a Go string constant so editing it doesn't mean reading around
// quoting, and it names only the tools an agent would not already have in its own tool list.
//
//go:embed briefing.md
var briefingDoc string

// mcpURLPlaceholder is where the briefing names the panel's own endpoint. It is
// substituted rather than formatted, since a %s in prose about a shell command line is
// too easy to write by accident.
const mcpURLPlaceholder = "{{MCP_URL}}"

// logPathPlaceholder is where the briefing names the service's log, found the same way
// the service that writes it does, so an agent sent looking for a failure is sent to the
// file that holds it.
const logPathPlaceholder = "{{LOG_PATH}}"

// Launcher turns an agent into a running CLI process. It owns what a spawned agent
// is told and nothing else, with no store, no lifecycle, and no knowledge of what
// an agent is. How any of it is spelled on a command line belongs to the harness,
// which arrives per launch because which CLI an agent runs is the agent's own.
type Launcher struct {
	tmux paneWriter
	// serverAddr is where serve listens, which is also where the MCP endpoint
	// lives. Agents are told about it at launch.
	serverAddr string
	// cacheDir is where a harness writes the temp files it passes a prompt through, and
	// logPath is the service's log, which the briefing sends an agent to.
	cacheDir string
	logPath  string
	// contextPath is the file holding the standing text every agent is started with. The
	// path is fixed at startup but the file is read per launch, so editing it reaches the
	// next agent without restarting the service.
	contextPath string
	// hookCommand is what an agent CLI's hooks run to reach the panel, which is this
	// binary. Empty leaves an agent launched with no hooks registered.
	hookCommand string
}

// LauncherParams is where a launcher reads the addresses and directories it hands on. Only
// Tmux has no sensible zero, since a launcher with nowhere to type cannot launch.
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

// buildBriefing is the doc with the paths this install actually runs on filled in.
func (l *Launcher) buildBriefing() string {
	text := strings.ReplaceAll(briefingDoc, mcpURLPlaceholder, fmt.Sprintf("http://%s/mcp", l.serverAddr))

	return strings.ReplaceAll(text, logPathPlaceholder, l.logPath)
}

// appendedSystemPrompt is what the panel has to say to an agent it is starting. The
// standing briefing and who spawned it are weave's own voice, so they come first rather
// than competing for the last word; the user's standing context and the workflow's follow,
// and between those two a workflow reads as the last word on anything the user's also
// speaks to.
func (l *Launcher) appendedSystemPrompt(workflowContext, spawnedByAgentID, spawnedByName string) string {
	parts := []string{l.buildBriefing(), delegateBriefing(spawnedByAgentID, spawnedByName), l.globalContext(), workflowContext}

	trimmed := xslice.Map(parts, strings.TrimSpace)
	kept := xslice.Filter(trimmed, func(s string) bool { return s != "" })

	return strings.Join(kept, "\n\n")
}

// delegateBriefing is what a spawned agent is told about the agent that started it, empty
// for one a person started from the panel. Every delegate gets the same wording whether or
// not the agent that spawned it thought to say so itself in the opening prompt.
func delegateBriefing(spawnedByAgentID, spawnedByName string) string {
	if spawnedByAgentID == "" {
		return ""
	}

	return fmt.Sprintf(
		"You were spawned by agent %s (%s) to do this piece of work. Answer its mail as it arrives, and tell it when you are done rather than waiting to be asked.",
		spawnedByName, spawnedByAgentID)
}

// globalContext is the user's standing text, empty when they keep none. A file that is not
// there is the ordinary case rather than a problem, and one that cannot be read is not
// worth refusing a launch over, so both come back empty.
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
	AgentID         string
	// SpawnedByAgentID and SpawnedByName are both empty for an agent a person started.
	SpawnedByAgentID string
	SpawnedByName    string
	Resume           bool
}

// launch types the command that puts an agent up into its pane. What that command
// is comes from the harness; whether the briefing survives a resume, and how the
// prompt reaches the CLI, are its business rather than the launcher's.
func (l *Launcher) launch(h harness.Harness, params launchParams) error {
	command, err := h.BuildCommand(harness.Launch{
		Flags:       params.Flags,
		Prompt:      params.Prompt,
		Dir:         params.Dir,
		Context:     l.appendedSystemPrompt(params.WorkflowContext, params.SpawnedByAgentID, params.SpawnedByName),
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
