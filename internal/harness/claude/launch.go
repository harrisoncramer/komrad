package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"weave/internal/caller"
	"weave/internal/harness"
)

const mcpConfigJSON = `{"mcpServers":{"weave":{"type":"http","url":"http://%s/mcp","alwaysLoad":true,` +
	`"headers":{"` + caller.AgentHeader + `":"${` + harness.AgentIDEnv + `:-}",` +
	`"` + caller.SessionHeader + `":"${` + harness.SessionIDEnv + `:-}"}}}}`

var deniedAgents = []string{"Agent(general-purpose)", "Agent(claude)"}

// BuildCommand renders the command line that puts Claude up, quoting everything interpolated
// since it is typed into a pane and parsed by the shell running there.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	command := harness.EnvPrefix(launch) + binary

	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " " + quoted
	}

	if path, err := h.writeMCPConfig(launch.MCPEndpoint); err == nil {
		command += fmt.Sprintf(" --mcp-config '%s'", path)
	}

	if path, err := h.writeSettings(launch.HookCommand); err == nil {
		command += fmt.Sprintf(" --settings '%s'", path)
	}

	switch {
	case launch.Resume && launch.SessionID != "":
		command += " --resume " + harness.QuoteArg(launch.SessionID)
	default:
		if launch.SessionID != "" {
			command += " --session-id " + harness.QuoteArg(launch.SessionID)
		}

		if launch.Context != "" {
			if path, err := harness.WriteTempText(launch.CacheDir, "claude-context-*.md", launch.Context); err == nil {
				command += fmt.Sprintf(" --append-system-prompt \"$(cat '%s'; rm '%s')\"", path, path)
			}
		}
	}

	if launch.Prompt != "" {
		path, err := harness.WriteTempText(launch.CacheDir, "claude-prompt-*.md", launch.Prompt)
		if err != nil {
			return "", err
		}

		command += fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", path, path)
	}

	return command, nil
}

func (h *Harness) writeSettings(hookCommand string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create the settings dir: %w", err)
	}

	settings := map[string]any{
		"permissions": map[string]any{"deny": deniedAgents},
	}
	if hookCommand != "" {
		settings["hooks"] = map[string]any{
			"Stop": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": hookCommand + " hook stop"}},
			}},
		}
	}

	body, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to encode the settings: %w", err)
	}

	path := filepath.Join(dir, "weave-settings.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("failed to write the settings: %w", err)
	}

	return path, nil
}

func (h *Harness) writeMCPConfig(endpoint string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
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
