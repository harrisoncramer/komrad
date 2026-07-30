package agents

import (
	"fmt"
	"strings"

	"claude-agents/internal/harness"
)

// launchTmux is all a launch needs from tmux, somewhere to type. The command is not
// executed directly: it is typed into a pane and parsed by the shell running there, which
// is why the harness that assembles it quotes everything interpolated into it.
type launchTmux interface {
	SendKeys(target string, keys ...string) error
}

// launchHarness is all a launch needs from the harness, the command line that puts
// the agent's CLI up.
type launchHarness interface {
	BuildCommand(harness.Launch) (string, error)
}

// Launcher turns an agent into a running CLI process. It owns what a spawned agent
// is told and nothing else, with no store, no lifecycle, and no knowledge of what
// an agent is. How any of it is spelled on a command line belongs to the harness,
// which arrives per launch because which CLI an agent runs is the agent's own.
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

// mcpBriefing tells a spawned agent that the panel's own database is queryable, so a
// question about the other agents is something it can look up rather than guess at. The
// closing line matters as much as the rest: an agent whose client has not registered the
// server must say so instead of inventing an answer.
const mcpBriefing = `You are a background agent run by claude-agents, a tmux panel that spawns and tracks background coding agents. The panel serves its own Postgres database read-only over MCP as a server named "claude-agents" at http://%s/mcp.

That server's tools are list_tables, describe_table, and query, which runs a single SELECT. Between them they cover every agent the panel knows about. The agents table holds each agent's name, state, pane, working directory, and flags, workflows the groups agents belong to, and agent_transcripts the text of their conversations. Writes are rejected. Reach for these when the question is about the other agents rather than the code in front of you.

You are one of those agents, and your own id is in the CLAUDE_AGENTS_AGENT_ID environment variable. As you work, record what a person would want to open later against that id with set_agent_metadata, under keys you choose: the Linear issue you are implementing, the pull request you opened, the branch you are on. Store full URLs. The panel shows them beside you, which is the only way someone finds their way back to what you touched without reading this whole conversation again. Record each one when you learn it rather than at the end, since a conversation can be interrupted.

If those tools are not present in your session, the server is not registered with your client. Say that plainly rather than answering from guesswork.`

// appendedSystemPrompt is the standing MCP briefing followed by the workflow's shared
// context, which is what the panel has to say to an agent it is starting.
func (l *Launcher) appendedSystemPrompt(workflowContext string) string {
	briefing := fmt.Sprintf(mcpBriefing, l.serverAddr)
	if strings.TrimSpace(workflowContext) == "" {
		return briefing
	}

	return briefing + "\n\n" + workflowContext
}

type launchParams struct {
	Pane            string
	Dir             string
	Flags           string
	Prompt          string
	WorkflowContext string
	SessionID       string
	AgentID         string
	Resume          bool
}

// launch types the command that puts an agent up into its pane. What that command
// is comes from the harness; whether the briefing survives a resume, and how the
// prompt reaches the CLI, are its business rather than the launcher's.
func (l *Launcher) launch(h launchHarness, params launchParams) error {
	command, err := h.BuildCommand(harness.Launch{
		Flags:       params.Flags,
		Prompt:      params.Prompt,
		Dir:         params.Dir,
		Context:     l.appendedSystemPrompt(params.WorkflowContext),
		SessionID:   params.SessionID,
		AgentID:     params.AgentID,
		MCPEndpoint: l.serverAddr,
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
