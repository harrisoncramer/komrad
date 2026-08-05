package agents

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"weave/internal/harness"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"
)

// briefingDoc is what every spawned agent is told about the panel running it: what it can
// look up rather than guess at, what the panel reads out of its pane, and where to find
// the log when the panel is behaving oddly. It stops at what an agent needs to work under
// weave, since most agents are working on something else entirely and how this repo builds
// itself is no use to them. It is prose in a file rather than a string constant because
// editing it should not mean reading around Go quoting, and it names only the tools an
// agent would not think to go looking for. The rest of the surface is in the tool list its
// client already has.
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

// briefedSiblings is how many of a workflow's other agents are named at launch. The rest are
// counted, since a long list stops being something an agent takes in at a glance.
const briefedSiblings = 8

// Launcher turns an agent into a running CLI process. It owns what a spawned agent is told
// and nothing else, with no lifecycle and no knowledge of what an agent is beyond the one
// read that tells it who else is in the workflow. How any of it is spelled on a command line
// belongs to the harness, which arrives per launch because which CLI an agent runs is the
// agent's own.
type Launcher struct {
	tmux paneWriter
	// agents is read per launch for the other agents in the workflow the launching agent is
	// joining. A launcher without one still launches, telling the agent nothing about them.
	agents workflowAgentReader
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
	Agents      workflowAgentReader
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
		agents:      p.Agents,
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
// standing briefing, who spawned it, and who it is starting alongside are weave's own voice,
// so they come first rather than competing for the last word; the user's standing context and
// the workflow's follow, and between those two a workflow reads as the last word on anything
// the user's also speaks to.
func (l *Launcher) appendedSystemPrompt(ctx context.Context, params launchParams) string {
	parts := []string{
		l.buildBriefing(),
		delegateBriefing(params.SpawnedByAgentID, params.SpawnedByName),
		l.buildSiblingBriefing(ctx, params.WorkflowID, params.AgentID),
		l.globalContext(),
		params.WorkflowContext,
	}

	trimmed := xslice.Map(parts, strings.TrimSpace)
	kept := xslice.Filter(trimmed, func(s string) bool { return s != "" })

	return strings.Join(kept, "\n\n")
}

// buildSiblingBriefing is who else was in the agent's workflow the moment it started, which is
// the one part of the prompt that is different every launch. It is a glance rather than a
// listing, so a workflow with more agents than that names the first few and counts the rest. A
// read that fails is not worth refusing a launch over, since the agent can ask the panel itself.
func (l *Launcher) buildSiblingBriefing(ctx context.Context, workflowID, agentID string) string {
	if l.agents == nil || workflowID == "" {
		return ""
	}

	live, err := l.agents.ListLiveInWorkflow(ctx, workflowID)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "could not read the agents already in a launching agent's workflow",
			append([]slog.Attr{slog.String(logging.WorkflowID, workflowID)}, logging.Err(err)...)...)

		return ""
	}

	siblings := xslice.Filter(live, func(agent agentstore.Agent) bool { return agent.ID != agentID })
	if len(siblings) == 0 {
		return "You are the only agent in your workflow. Anything you want done alongside you, you have to spawn yourself."
	}

	named := siblings
	if len(named) > briefedSiblings {
		named = named[:briefedSiblings]
	}

	listed := strings.Join(xslice.Map(named, describeSibling), ", ")
	if len(named) < len(siblings) {
		listed = fmt.Sprintf("%s, and %d more", listed, len(siblings)-len(named))
	}

	return fmt.Sprintf("These agents were in your workflow when you started, with what the panel last saw each of them doing: %s. That is a snapshot rather than the current picture, and it does not say what any of them is working on, so use list_agents and their transcripts before you act on it.", listed)
}

func describeSibling(agent agentstore.Agent) string {
	return fmt.Sprintf("%s (%s)", agent.Name, agent.State)
}

// delegateBriefing is what a spawned agent is told about the agent that started it, empty
// for one a person started from the panel. Every delegate gets the same wording whether or
// not the agent that spawned it thought to say so itself in the opening prompt.
func delegateBriefing(spawnedByAgentID, spawnedByName string) string {
	if spawnedByAgentID == "" {
		return ""
	}

	return fmt.Sprintf(
		"You were spawned by agent %s (%s) to do this piece of work. Answer its messages as they arrive, and tell it when you are done rather than waiting to be asked.",
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
	WorkflowID      string
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
func (l *Launcher) launch(ctx context.Context, h harness.Harness, params launchParams) error {
	command, err := h.BuildCommand(harness.Launch{
		Flags:       params.Flags,
		Prompt:      params.Prompt,
		Dir:         params.Dir,
		Context:     l.appendedSystemPrompt(ctx, params),
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
