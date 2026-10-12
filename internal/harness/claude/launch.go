package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/lib/xfile"
)

const mcpConfigJSON = `{"mcpServers":{"komrad":{"type":"http","url":"http://%s/mcp","alwaysLoad":true,` +
	`"headers":{"` + caller.AgentHeader + `":"${` + harness.AgentIDEnv + `:-}",` +
	`"` + caller.SessionHeader + `":"${` + harness.SessionIDEnv + `:-}"}}}}`

const minAutoCompactWindow = 100_000

const maxAutoCompactWindow = 1_000_000

var deniedAgents = []string{"Agent(general-purpose)", "Agent(claude)"}

var allowedTools = []string{"mcp__komrad"}

// BuildCommand renders the command line that puts Claude up, quoting everything interpolated
// since it is written to a script and parsed by the shell komrad starts the pane with.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	command := harness.EnvPrefix(launch)
	if h.configDir != "" {
		command += configDirEnv + "=" + harness.QuoteArg(h.configDir) + " "
	}
	command += h.Binary()

	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " " + quoted
	}

	mcpConfig, err := h.writeMCPConfig(launch.MCPEndpoint)
	if err != nil {
		return "", err
	}
	command += fmt.Sprintf(" --mcp-config '%s'", mcpConfig)

	settings, err := h.writeSettings(launch)
	if err != nil {
		return "", err
	}
	command += fmt.Sprintf(" --settings '%s'", settings)

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

func (h *Harness) writeSettings(launch harness.Launch) (string, error) {
	configDir, err := h.resolveConfigDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create the settings dir: %w", err)
	}

	settings := map[string]any{
		"permissions": map[string]any{"allow": allowedTools, "deny": deniedAgents},
	}
	if hooks := hookSettings(launch.HookCommand); len(hooks) > 0 {
		settings["hooks"] = hooks
	}
	if launch.AutoCompaction > 0 {
		settings["autoCompactWindow"] = min(max(launch.AutoCompaction, minAutoCompactWindow), maxAutoCompactWindow)
	}

	body, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to encode the settings: %w", err)
	}

	path := filepath.Join(configDir, settingsFileName(launch.MCPEndpoint, launch.Role))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("failed to write the settings: %w", err)
	}

	return path, nil
}

func settingsFileName(endpoint, role string) string {
	name := "komrad-settings"
	for _, part := range []string{endpoint, role} {
		if part != "" {
			name += "-" + fileNamePart(part)
		}
	}

	return name + ".json"
}

// hookInstall is how komrad answers one Claude event, kept a struct so an event needing a matcher
// of its own is a field here.
type hookInstall struct {
	Subcommand string
	Matcher    string
}

// InstalledHooks is the events komrad answers, keyed by the event Claude fires.
var InstalledHooks = map[harness.Event]hookInstall{
	EventStop:               {Subcommand: "stop"},
	EventStopFailure:        {Subcommand: "stop-failure"},
	EventPreToolUse:         {Subcommand: "pre-tool-use"},
	EventPostToolUse:        {Subcommand: "post-tool-use"},
	EventPostToolUseFailure: {Subcommand: "post-tool-use-failure"},
	EventPermissionRequest:  {Subcommand: "permission-request"},
	EventSessionStart: {
		Subcommand: "compacted",
		Matcher:    compactMatcher,
	},
}

// hookSettings is the hooks block Claude loads at session start, empty when komrad has no binary
// to point the events at.
func hookSettings(hookCommand string) map[string]any {
	if hookCommand == "" {
		return nil
	}

	hooks := make(map[string]any, len(InstalledHooks))
	for event, install := range InstalledHooks {
		group := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": hookCommand + " hook " + install.Subcommand}},
		}
		if install.Matcher != "" {
			group["matcher"] = install.Matcher
		}
		hooks[string(event)] = []any{group}
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

	path := filepath.Join(configDir, mcpConfigFileName(endpoint))
	if err := xfile.WriteAtomic(path, []byte(fmt.Sprintf(mcpConfigJSON, endpoint)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return path, nil
}

func mcpConfigFileName(endpoint string) string {
	return fmt.Sprintf("komrad-mcp-%s.json", fileNamePart(endpoint))
}

func fileNamePart(text string) string {
	return strings.Map(func(char rune) rune {
		if char == '.' || char == '-' || unicode.IsLetter(char) || unicode.IsDigit(char) {
			return char
		}

		return '-'
	}, text)
}
