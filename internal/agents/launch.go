package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// launchTmux is all a launch needs from tmux, somewhere to type. The command is
// not executed directly. It is typed into a pane and parsed by the shell running
// there, which is why everything interpolated into it is either generated here or
// quoted.
type launchTmux interface {
	SendKeys(target string, keys ...string) error
}

// Launcher turns an agent into a running claude process. It owns the command
// line and nothing else. No store, no lifecycle, no knowledge of what an agent
// is.
type Launcher struct {
	tmux launchTmux
	// serverAddr is where serve listens, which is also where the MCP endpoint
	// lives. Agents are told about it at launch.
	serverAddr string
}

// NewLauncher builds a launcher that points spawned agents at serverAddr.
func NewLauncher(tmux launchTmux, serverAddr string) *Launcher {
	if serverAddr == "" {
		serverAddr = defaultServerAddr
	}

	return &Launcher{tmux: tmux, serverAddr: serverAddr}
}

// mcpBriefing tells a spawned agent that the panel's own database is queryable,
// so a question about the other agents is something it can look up rather than
// guess at. The closing line matters as much as the rest. An agent whose client
// has not registered the server must say so instead of inventing an answer.
const mcpBriefing = `You are a background agent run by claude-agents, a tmux panel that spawns and tracks Claude sessions. The panel serves its own Postgres database read-only over MCP as a server named "claude-agents" at http://%s/mcp.

That server's tools are list_tables, describe_table, and query, which runs a single SELECT. Between them they cover every agent the panel knows about. The agents table holds each agent's name, state, and pane, agent_configs its working directory and flags, workflows the groups agents belong to, and agent_transcripts the text of their conversations. Writes are rejected. Reach for these when the question is about the other agents rather than the code in front of you.

You are one of those agents, and your own id is in the CLAUDE_AGENTS_AGENT_ID environment variable. As you work, record what a person would want to open later against that id with set_agent_metadata, under keys you choose: the Linear issue you are implementing, the pull request you opened, the branch you are on. Store full URLs. The panel shows them beside you, which is the only way someone finds their way back to what you touched without reading this whole conversation again. Record each one when you learn it rather than at the end, since a conversation can be interrupted.

If those tools are not present in your session, the server is not registered with your client. Say that plainly rather than answering from guesswork.`

// mcpConfigJSON is the MCP client config every agent launches with, pointing at
// the panel's own read-only endpoint. It is passed with --mcp-config rather than
// --strict-mcp-config, so an agent gains this server on top of whatever the user
// has already registered instead of trading them for it.
const mcpConfigJSON = `{"mcpServers":{"claude-agents":{"type":"http","url":"http://%s/mcp"}}}`

// writeMCPConfig writes the MCP client config beside the panel's other state and
// returns its path. It is rewritten on every launch so a changed listen address
// takes effect without a stale file lingering. Unlike the prompt temp files it
// is not removed as the command runs, since a resumed agent re-reads it at every
// relaunch.
func (l *Launcher) writeMCPConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create mcp config dir: %w", err)
	}

	path := filepath.Join(dir, "claude-agents-mcp.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(mcpConfigJSON, l.serverAddr)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return path, nil
}

// appendedSystemPrompt is the text a fresh launch appends to the system prompt.
// The standing MCP briefing, followed by the workflow's shared context when the
// agent belongs to one.
func (l *Launcher) appendedSystemPrompt(workflowContext string) string {
	briefing := fmt.Sprintf(mcpBriefing, l.serverAddr)
	if strings.TrimSpace(workflowContext) == "" {
		return briefing
	}

	return briefing + "\n\n" + workflowContext
}

// launchClaudeParams configures a single claude launch.
type launchClaudeParams struct {
	Pane            string
	Flags           string
	Prompt          string
	WorkflowContext string
	ClaudeSessionID string
	AgentID         string
	Resume          bool
}

// launch types the claude command into a pane. When Resume is set it resumes the
// prior conversation by claude session id. Otherwise it starts a new
// conversation pinned to that id, with the opening prompt passed through a temp
// file so multi-line input survives, removed as the command runs.
func (l *Launcher) launch(params launchClaudeParams) error {
	command := "claude"

	// The agent's own id rides in on the environment rather than in the appended
	// system prompt, because it has to survive a resume and that prompt does not. An
	// agent that only learned its id at spawn would go anonymous the first time it was
	// restored and could no longer record anything against itself. It is a uuid out of
	// the store, quoted like everything else that reaches this shell.
	if params.AgentID != "" {
		command = fmt.Sprintf("CLAUDE_AGENTS_AGENT_ID=%s claude", quoteArg(params.AgentID))
	}

	// Flags are data, not shell. They are split into arguments and quoted, so a
	// flag carrying $(...), a backtick, or a semicolon reaches claude as a
	// literal argument instead of running in the pane's shell. Flags can arrive
	// over RPC, which makes this the boundary between a request and the
	// user's machine.
	if quoted := quoteFlags(params.Flags); quoted != "" {
		command += " " + quoted
	}

	// Both paths get the MCP config. The briefing does not survive a resume, but
	// the tools it describes should. A failure to write it drops the flag rather
	// than the launch, leaving an agent that runs without the server.
	if path, err := l.writeMCPConfig(); err == nil {
		command += fmt.Sprintf(" --mcp-config '%s'", path)
	}

	// A restored agent's claude session id comes back out of the store, so it is
	// quoted like anything else that was not built inline.
	switch {
	case params.Resume && params.ClaudeSessionID != "":
		command += " --resume " + quoteArg(params.ClaudeSessionID)
	default:
		if params.ClaudeSessionID != "" {
			command += " --session-id " + quoteArg(params.ClaudeSessionID)
		}
		// Pass the MCP briefing and any workflow context as an appended system
		// prompt, sourced through a temp file the shell cats in place so arbitrary
		// markdown needs no escaping. --append-system-prompt does not survive
		// --resume, matching the snapshot-at-spawn behavior.
		file, err := os.CreateTemp("", "claude-context-*.md")
		if err == nil {
			_, _ = file.WriteString(l.appendedSystemPrompt(params.WorkflowContext))
			_ = file.Close()
			command += fmt.Sprintf(" --append-system-prompt \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
		}
		// The opening prompt is positional, and --mcp-config takes a variable
		// number of paths, so the prompt is fenced off with -- or a launch where
		// no flag separates the two would read the prompt as another config file.
		if params.Prompt != "" {
			file, err := os.CreateTemp("", "claude-prompt-*.md")
			if err == nil {
				_, _ = file.WriteString(params.Prompt)
				_ = file.Close()
				command += fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
			}
		}
	}

	if err := l.tmux.SendKeys(params.Pane, command, "Enter"); err != nil {
		return fmt.Errorf("failed to type the launch command: %w", err)
	}

	return nil
}
