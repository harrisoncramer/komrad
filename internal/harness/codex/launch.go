package codex

import (
	"fmt"
	"strings"

	"weave/internal/harness"
)

// BuildCommand renders the command line that puts Codex up. It is not executed
// directly: it is typed into a pane and parsed by the shell running there, which is
// why everything interpolated into it is either generated here or quoted.
//
// A resume reopens the prior conversation by id. Otherwise it starts a new one,
// with the opening prompt passed through a temp file so multi-line markdown
// survives, removed as the command runs.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	// Recorded before anything is assembled, since the launch itself cannot carry the
	// answer and an agent that meets the question sits at it.
	if err := h.trustDir(launch.Dir); err != nil {
		return "", err
	}

	command := "codex"
	if launch.AgentID != "" {
		command = fmt.Sprintf("%s=%s codex", harness.AgentIDEnv, harness.QuoteArg(launch.AgentID))
	}

	resuming := launch.Resume && launch.SessionID != ""
	if resuming {
		command += " resume"
	}

	// Flags are data, not shell. They are split into arguments and quoted, so a flag
	// carrying $(...), a backtick, or a semicolon reaches codex as a literal argument
	// instead of running in the pane's shell. They can arrive over RPC, which makes this
	// the boundary between a request and the user's machine.
	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " " + quoted
	}

	// Codex takes MCP servers from its config rather than a flag, so the panel's server
	// is overridden for this invocation alone. Both paths carry it, since a resumed
	// agent should keep the tools.
	if launch.MCPEndpoint != "" {
		command += " -c " + harness.QuoteArg(fmt.Sprintf(
			"mcp_servers.weave.url=%s",
			tomlString("http://"+launch.MCPEndpoint+"/mcp"),
		))
	}

	if resuming {
		return command + " " + harness.QuoteArg(launch.SessionID), nil
	}

	prompt := h.openingPrompt(launch)
	if prompt == "" {
		return command, nil
	}

	// The prompt is positional and could open with a word codex reads as a subcommand, so
	// it is fenced off with --. It is sourced through a temp file the shell cats in place,
	// so arbitrary markdown needs no escaping.
	path, err := harness.WriteTempText("codex-prompt-*.md", prompt)
	if err != nil {
		return "", err
	}

	return command + fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", path, path), nil
}

// openingPrompt is the panel's briefing followed by the prompt the agent was created
// with. Codex has no way to append to its system prompt. The one config override that
// looks like it, instructions, replaces the CLI's own instructions wholesale and would
// leave the agent without the rules it needs to use its tools. So the briefing is the
// first thing said instead, which means it does not survive a resume. The agent's own id
// does, since that rides on the environment.
//
// An agent created with no prompt is left unbriefed rather than handed the briefing
// alone, which it would answer as though it were the task.
func (h *Harness) openingPrompt(launch harness.Launch) string {
	if launch.Prompt == "" {
		return ""
	}
	if strings.TrimSpace(launch.Context) == "" {
		return launch.Prompt
	}

	// Tagged so the flattener can drop it again. Because the briefing arrives as a
	// message, Codex records it in the rollout like anything else, and left untagged it
	// would be indexed as the longest thing the agent ever said and answer searches for
	// every phrase in it. Codex fences its own injected context the same way.
	return briefingOpen + "\n" + launch.Context + "\n" + briefingClose + "\n\n" + launch.Prompt
}

// tomlString renders a value as a TOML basic string, which is what a -c override's
// value is parsed as. A path or a URL that is not valid TOML would otherwise be taken
// as a bare literal, and one carrying a quote would end the value early.
func tomlString(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)

	return `"` + escaped + `"`
}
