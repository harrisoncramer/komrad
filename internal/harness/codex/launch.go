package codex

import (
	"fmt"
	"strings"

	"komrad/internal/caller"
	"komrad/internal/harness"
)

// BuildCommand renders the script that starts the agent's own app server and points Codex at it.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	// Explainer: the app server runs in the pane under the agent's environment, so the agent's
	// commands and its calls to komrad carry its identity, and komrad reaches the agent's thread
	// through the server's socket. Everything interpolated is quoted, since the shell komrad
	// starts the pane with parses the script.
	socket, err := harness.PrepareSocket(h.cacheDir, HarnessID, launch.AgentID)
	if err != nil {
		return "", err
	}
	endpoint := harness.QuoteArg("unix://" + socket)
	env := harness.EnvPrefix(launch) + h.homePrefix() + originatorPrefix(launch.AgentID)

	codex, err := h.codexCommand(launch, env, endpoint)
	if err != nil {
		return "", err
	}

	lines := []string{
		"rm -f " + harness.QuoteArg(socket),
		`( trap 'kill "$app" 2>/dev/null; sleep 0.5; kill "$app" 2>/dev/null' HUP TERM; ` +
			env + harness.RunBinary(HarnessID) + " app-server --listen " + endpoint + configFlags(launch) +
			` >/dev/null 2>&1 & app=$!; wait "$app" ) &`,
		"server=$!",
		"while [ ! -S " + harness.QuoteArg(socket) + ` ] && kill -0 "$server" 2>/dev/null; do sleep 0.1; done`,
		codex,
		`kill "$server" 2>/dev/null`,
	}

	return strings.Join(lines, "\n"), nil
}

// configFlags is what komrad sets on the app server, which runs the agent's tools and hooks.
func configFlags(launch harness.Launch) string {
	var flags string
	if launch.MCPEndpoint != "" {
		flags += " -c " + harness.QuoteArg(fmt.Sprintf(
			"mcp_servers.komrad.url=%s",
			tomlString("http://"+launch.MCPEndpoint+"/mcp"),
		))
		flags += " -c " + harness.QuoteArg(mcpHeaders())
		flags += " -c " + harness.QuoteArg(`mcp_servers.komrad.default_tools_approval_mode="approve"`)
	}

	if launch.HookCommand != "" {
		flags += " -c " + harness.QuoteArg(notifyProgram(launch.HookCommand))
	}

	if launch.AutoCompaction > 0 {
		flags += " -c " + harness.QuoteArg(fmt.Sprintf("model_auto_compact_token_limit=%d", launch.AutoCompaction))
	}

	return flags
}

// codexCommand is the line that opens Codex on the pane's app server.
func (h *Harness) codexCommand(launch harness.Launch, env, endpoint string) (string, error) {
	command := env + harness.RunBinary(HarnessID)

	resuming := launch.Resume && launch.SessionID != ""
	if resuming {
		command += " resume"
	}

	command += " --remote " + endpoint

	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " " + quoted
	}

	if resuming {
		command += " " + harness.QuoteArg(launch.SessionID)
		if launch.Prompt == "" {
			return command, nil
		}

		path, err := harness.WriteTempText(launch.CacheDir, "codex-prompt-*.md", launch.Prompt)
		if err != nil {
			return "", err
		}

		return command + fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", path, path), nil
	}

	prompt := h.openingPrompt(launch)
	if prompt == "" {
		return command, nil
	}

	path, err := harness.WriteTempText(launch.CacheDir, "codex-prompt-*.md", prompt)
	if err != nil {
		return "", err
	}

	return command + fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", path, path), nil
}

// notifyProgram is codex's only report of a turn ending, which it calls with one JSON argument.
func notifyProgram(hookCommand string) string {
	return fmt.Sprintf("notify=[%s,%s,%s]", tomlString(hookCommand), tomlString("hook"), tomlString("turn-end"))
}

func mcpHeaders() string {
	return fmt.Sprintf(
		"mcp_servers.komrad.env_http_headers={%s=%s,%s=%s}",
		tomlString(caller.AgentHeader), tomlString(harness.AgentIDEnv),
		tomlString(caller.SessionHeader), tomlString(harness.SessionIDEnv),
	)
}

func (h *Harness) openingPrompt(launch harness.Launch) string {
	if launch.Prompt == "" {
		return ""
	}
	if strings.TrimSpace(launch.Context) == "" {
		return launch.Prompt
	}

	return briefingOpen + "\n" + launch.Context + "\n" + briefingClose + "\n\n" + launch.Prompt
}

func tomlString(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)

	return `"` + escaped + `"`
}
