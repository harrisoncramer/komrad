package claude

import (
	"fmt"
	"os"
	"path/filepath"

	"weave/internal/caller"
	"weave/internal/harness"
)

// mcpConfigJSON is the MCP client config every agent launches with, pointing at
// the panel's own read-only endpoint. It is passed with --mcp-config rather than
// --strict-mcp-config, so an agent gains this server on top of whatever the user
// has already registered instead of trading them for it.
//
// The headers are what let the service tell one caller from another. They are written as
// variables rather than as this agent's own ids, because Claude expands them out of the
// environment it is launched with, which the panel has already put the ids into. That is
// what keeps this one shared file: an agent fills in its own identity as it starts, so two
// agents reading the same file still call back as themselves. The :- default leaves the
// header empty rather than literal when a variable is unset.
const mcpConfigJSON = `{"mcpServers":{"weave":{"type":"http","url":"http://%s/mcp",` +
	`"headers":{"` + caller.AgentHeader + `":"${` + harness.AgentIDEnv + `:-}",` +
	`"` + caller.SessionHeader + `":"${` + harness.SessionIDEnv + `:-}"}}}}`

// BuildCommand renders the command line that puts Claude up. It is not executed
// directly: it is typed into a pane and parsed by the shell running there, which
// is why everything interpolated into it is either generated here or quoted.
//
// A resume reopens the prior conversation by session id. Otherwise it starts a new
// conversation pinned to that id, with the appended system prompt and the opening
// prompt passed through temp files so multi-line markdown survives, removed as the
// command runs.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	// The agent's own ids are exported ahead of the CLI, which is both how it can name
	// itself to the panel and how the MCP config below resolves the headers it sends.
	command := harness.EnvPrefix(launch) + "claude"

	// Flags are data, not shell. They are split into arguments and quoted, so a flag
	// carrying $(...), a backtick, or a semicolon reaches claude as a literal argument
	// instead of running in the pane's shell. They can arrive over RPC, which makes this
	// the boundary between a request and the user's machine.
	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " " + quoted
	}

	// Both paths get the MCP config: the briefing describing it does not survive a
	// resume, but the tools it describes should. A failure to write it drops the flag
	// rather than the launch.
	if path, err := h.writeMCPConfig(launch.MCPEndpoint); err == nil {
		command += fmt.Sprintf(" --mcp-config '%s'", path)
	}

	switch {
	case launch.Resume && launch.SessionID != "":
		command += " --resume " + harness.QuoteArg(launch.SessionID)
	default:
		if launch.SessionID != "" {
			command += " --session-id " + harness.QuoteArg(launch.SessionID)
		}
		// The appended system prompt is sourced through a temp file the shell cats in place,
		// so arbitrary markdown needs no escaping.
		if launch.Context != "" {
			if path, err := harness.WriteTempText("claude-context-*.md", launch.Context); err == nil {
				command += fmt.Sprintf(" --append-system-prompt \"$(cat '%s'; rm '%s')\"", path, path)
			}
		}
		// The opening prompt is positional and --mcp-config takes a variable number of paths,
		// so the prompt is fenced off with -- or a launch where no flag separates the two would
		// read the prompt as another config file.
		if launch.Prompt != "" {
			if path, err := harness.WriteTempText("claude-prompt-*.md", launch.Prompt); err == nil {
				command += fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", path, path)
			}
		}
	}

	return command, nil
}

// writeMCPConfig writes the MCP client config beside Claude's other state and
// returns its path. It is rewritten on every launch so a changed listen address
// takes effect, and unlike the prompt temp files it is not removed as the command
// runs, since a resumed agent re-reads it at every relaunch.
func (h *Harness) writeMCPConfig(endpoint string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create mcp config dir: %w", err)
	}

	path := filepath.Join(dir, "weave-mcp.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(mcpConfigJSON, endpoint)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return path, nil
}
