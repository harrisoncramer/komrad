package codex

import (
	"fmt"
	"strings"

	"weave/internal/caller"
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
	if err := h.trustDir(launch.Dir); err != nil {
		return "", err
	}

	command := harness.EnvPrefix(launch) + originatorPrefix(launch.AgentID) + binary

	resuming := launch.Resume && launch.SessionID != ""
	if resuming {
		command += " resume"
	}

	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " " + quoted
	}

	if launch.MCPEndpoint != "" {
		command += " -c " + harness.QuoteArg(fmt.Sprintf(
			"mcp_servers.weave.url=%s",
			tomlString("http://"+launch.MCPEndpoint+"/mcp"),
		))
		command += " -c " + harness.QuoteArg(mcpHeaders())
	}

	if resuming {
		return command + " " + harness.QuoteArg(launch.SessionID), nil
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

func mcpHeaders() string {
	return fmt.Sprintf(
		"mcp_servers.weave.env_http_headers={%s=%s,%s=%s}",
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
