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
// since it is written to a script and parsed by the shell weave starts the pane with.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	command := harness.EnvPrefix(launch)
	if h.configDir != "" {
		command += configDirEnv + "=" + harness.QuoteArg(h.configDir) + " "
	}
	command += h.Binary()

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
	configDir, err := h.resolveConfigDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create the settings dir: %w", err)
	}

	settings := map[string]any{
		"permissions": map[string]any{"deny": deniedAgents},
	}
	if hooks := hookSettings(hookCommand); len(hooks) > 0 {
		settings["hooks"] = hooks
	}

	body, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to encode the settings: %w", err)
	}

	path := filepath.Join(configDir, "weave-settings.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("failed to write the settings: %w", err)
	}

	return path, nil
}

// HookInstall is how weave answers one Claude event, kept a struct so an event needing a matcher
// of its own is a field here.
type HookInstall struct {
	Subcommand string
}

// InstalledHooks is the events weave answers, keyed by the event Claude fires.
var InstalledHooks = map[harness.Event]HookInstall{
	EventStop:              {Subcommand: "stop"},
	EventPreToolUse:        {Subcommand: "pre-tool-use"},
	EventPostToolUse:       {Subcommand: "post-tool-use"},
	EventPermissionRequest: {Subcommand: "permission-request"},
}

// hookSettings is the hooks block Claude loads at session start, empty when weave has no binary
// to point the events at.
func hookSettings(hookCommand string) map[string]any {
	if hookCommand == "" {
		return nil
	}

	hooks := make(map[string]any, len(InstalledHooks))
	for event, install := range InstalledHooks {
		hooks[string(event)] = []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": hookCommand + " hook " + install.Subcommand}},
		}}
	}

	return hooks
}

func (h *Harness) writeMCPConfig(endpoint string) (string, error) {
	configDir, err := h.resolveConfigDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create mcp config dir: %w", err)
	}

	path := filepath.Join(configDir, "weave-mcp.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(mcpConfigJSON, endpoint)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return path, nil
}
