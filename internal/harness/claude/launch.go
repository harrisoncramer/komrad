package claude

import (
	"fmt"
	"os"
	"path/filepath"

	"claude-agents/internal/harness"
)

// mcpConfigJSON is the MCP client config every agent launches with, pointing at
// the panel's own read-only endpoint. It is passed with --mcp-config rather than
// --strict-mcp-config, so an agent gains this server on top of whatever the user
// has already registered instead of trading them for it.
const mcpConfigJSON = `{"mcpServers":{"claude-agents":{"type":"http","url":"http://%s/mcp"}}}`

// Command renders the command line that puts Claude up. It is not executed
// directly: it is typed into a pane and parsed by the shell running there, which
// is why everything interpolated into it is either generated here or quoted.
//
// A resume reopens the prior conversation by session id. Otherwise it starts a new
// conversation pinned to that id, with the appended system prompt and the opening
// prompt passed through temp files so multi-line markdown survives, removed as the
// command runs.
func (h *Harness) Command(launch harness.Launch) (string, error) {
	command := "claude"

	if launch.AgentID != "" {
		command = fmt.Sprintf("%s=%s claude", harness.AgentIDEnv, harness.QuoteArg(launch.AgentID))
	}

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
			if path, err := writeTempText("claude-context-*.md", launch.Context); err == nil {
				command += fmt.Sprintf(" --append-system-prompt \"$(cat '%s'; rm '%s')\"", path, path)
			}
		}
		// The opening prompt is positional and --mcp-config takes a variable number of paths,
		// so the prompt is fenced off with -- or a launch where no flag separates the two would
		// read the prompt as another config file.
		if launch.Prompt != "" {
			if path, err := writeTempText("claude-prompt-*.md", launch.Prompt); err == nil {
				command += fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", path, path)
			}
		}
	}

	return command, nil
}

// writeTempText writes text to a temp file matching pattern and returns its path.
// The file is removed by the command that reads it rather than here, since nothing
// on this side of send-keys knows when the shell has run it.
func writeTempText(pattern, text string) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create %q: %w", pattern, err)
	}

	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()

		return "", fmt.Errorf("failed to write %q: %w", file.Name(), err)
	}

	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close %q: %w", file.Name(), err)
	}

	return file.Name(), nil
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

	path := filepath.Join(dir, "claude-agents-mcp.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(mcpConfigJSON, endpoint)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return path, nil
}
