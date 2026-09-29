package agents

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"komrad/config"
	"komrad/internal/harness"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xslice"
)

//go:embed briefing.md
var briefingDoc string

//go:embed briefing_implementer.md
var implementerDoc string

//go:embed briefing_orchestrator.md
var orchestratorDoc string

//go:embed briefing_orchestrator_context.md
var orchestratorContextDoc string

//go:embed briefing_reviewer.md
var reviewerDoc string

const mcpURLPlaceholder = "{{MCP_URL}}"

const logPathPlaceholder = "{{LOG_PATH}}"

// fallbackShell is what an agent is started under when tmux names no default shell.
const fallbackShell = "/bin/sh"

type NewLauncherParams struct {
	Tmux        paneStarter
	Addr        string
	CacheDir    string
	LogPath     string
	ContextPath string
	// BinPath is komrad's own binary, which an agent's hooks and its gh stand-in both run.
	BinPath         string
	Usage           config.Usage
	TrustLaunchDirs map[string]bool
}

type Launcher struct {
	tmux            paneStarter
	serverAddr      string
	cacheDir        string
	logPath         string
	contextPath     string
	binPath         string
	usage           config.Usage
	trustLaunchDirs map[string]bool
}

// NewLauncher builds a launcher that points spawned agents at the given address.
func NewLauncher(params NewLauncherParams) *Launcher {
	if params.Addr == "" {
		params.Addr = defaultServerAddr
	}

	return &Launcher{
		tmux:            params.Tmux,
		serverAddr:      params.Addr,
		cacheDir:        params.CacheDir,
		logPath:         params.LogPath,
		contextPath:     params.ContextPath,
		binPath:         params.BinPath,
		usage:           params.Usage,
		trustLaunchDirs: params.TrustLaunchDirs,
	}
}

func (l *Launcher) buildBriefing() string {
	text := strings.ReplaceAll(briefingDoc, mcpURLPlaceholder, fmt.Sprintf("http://%s/mcp", l.serverAddr))

	return strings.ReplaceAll(text, logPathPlaceholder, l.logPath)
}

func (l *Launcher) appendedSystemPrompt(ctx context.Context, params launchParams) string {
	parts := []string{
		l.buildBriefing(),
		roleBriefing(params),
		relationBriefing(params),
		l.globalContext(ctx),
		contextBriefing(params),
		params.WorkflowContext,
	}

	trimmed := xslice.Map(parts, strings.TrimSpace)
	kept := xslice.Filter(trimmed, func(s string) bool { return s != "" })

	return strings.Join(kept, "\n\n")
}

// roleBriefing is what an agent is told about the job it was launched to do.
func roleBriefing(params launchParams) string {
	switch {
	case params.Role == agentstore.RoleOrchestrator:
		return orchestratorDoc
	case params.Role == agentstore.RoleReviewer:
		return reviewerDoc
	case params.OwnsCheckout:
		return implementerDoc
	}

	return ""
}

// contextBriefing is what an agent is told about the workflow context coming after it.
func contextBriefing(params launchParams) string {
	if params.Role != agentstore.RoleOrchestrator || strings.TrimSpace(params.WorkflowContext) == "" {
		return ""
	}

	return orchestratorContextDoc
}

// relationBriefing is what an agent is told about whoever it answers to, and for a child the
// rules it runs under as well.
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

func (l *Launcher) globalContext(ctx context.Context) string {
	if l.contextPath == "" {
		return ""
	}

	text, err := os.ReadFile(l.contextPath)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the standing context could not be read, so this agent starts without it",
			append(logging.Err(err), slog.String("path", l.contextPath))...)

		return ""
	}

	return string(text)
}

// toolsDir is where komrad's stand-ins for the tools an agent calls are written, empty when
// they could not be.
func (l *Launcher) toolsDir(ctx context.Context) string {
	dir, err := harness.WriteGHShim(l.cacheDir, l.binPath)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "gh could not be given a stand-in, so this agent reaches github as the machine is logged in",
			logging.Err(err)...)

		return ""
	}

	return dir
}

func (l *Launcher) getAutoCompaction(role agentstore.Role) int {
	switch role {
	case agentstore.RoleReviewer:
		return l.usage.Reviewer.AutoCompaction
	case agentstore.RoleOrchestrator:
		return l.usage.Orchestrator.AutoCompaction
	case agentstore.RoleImplementer:
		return l.usage.Implementer.AutoCompaction
	default:
		return 0
	}
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
	Role             agentstore.Role

	// OwnsCheckout is whether komrad provisioned this agent a tree of its own, which separates an
	// agent writing a change from one reading somebody else's.
	OwnsCheckout bool
	Resume       bool
}

func (l *Launcher) launch(ctx context.Context, h harness.Harness, params launchParams) error {
	role := params.Role
	if role == "" {
		role = agentstore.RoleImplementer
	}

	if l.trustLaunchDirs[h.ID()] && params.Dir != "" {
		if err := h.TrustDir(params.Dir); err != nil {
			return fmt.Errorf("failed to trust the launch directory: %w", err)
		}
	}

	command, err := h.BuildCommand(harness.Launch{
		ToolsDir:       l.toolsDir(ctx),
		Flags:          params.Flags,
		Prompt:         params.Prompt,
		Dir:            params.Dir,
		Context:        l.appendedSystemPrompt(ctx, params),
		SessionID:      params.SessionID,
		AgentID:        params.AgentID,
		MCPEndpoint:    l.serverAddr,
		CacheDir:       l.cacheDir,
		HookCommand:    l.binPath,
		Resume:         params.Resume,
		Role:           string(role),
		AutoCompaction: l.getAutoCompaction(role),
	})
	if err != nil {
		return fmt.Errorf("failed to build the launch command: %w", err)
	}

	path, err := harness.WriteTempText(l.cacheDir, "launch-*.sh", command+"\n")
	if err != nil {
		return fmt.Errorf("failed to write the launch script: %w", err)
	}

	shell := l.tmux.GetDefaultShell()
	if shell == "" {
		shell = fallbackShell
	}

	script := ". " + harness.QuoteArg(path) + "; exec " + harness.QuoteArg(shell) + " -l -i"
	if err := l.tmux.RespawnPane(params.Pane, params.Dir, shell, "-l", "-i", "-c", script); err != nil {
		return fmt.Errorf("failed to start the agent in its pane: %w", err)
	}

	return nil
}
