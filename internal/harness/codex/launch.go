package codex

import (
	"fmt"
	"strings"

	"weave/internal/caller"
	"weave/internal/harness"
)

// BuildCommand renders the command line that puts Codex up, quoting everything interpolated
// since it is typed into a pane and parsed by the shell running there.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	if err := h.trustDir(launch.Dir); err != nil {
		return "", err
	}

	command := harness.EnvPrefix(launch) + originatorPrefix(launch.AgentID) + binary

	// Codex takes no session id of its own, so adopting a conversation resumes the person's
	// rollout rather than a copy of it, and the briefing has to go in as an opening prompt
	// because a resume carries no system prompt.
	adopting := launch.ResumeFrom != ""
	sessionID := launch.SessionID
	if adopting {
		sessionID = launch.ResumeFrom
	}

	resuming := adopting || (launch.Resume && launch.SessionID != "")
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
		command += " -c " + harness.QuoteArg(`mcp_servers.weave.default_tools_approval_mode="approve"`)
	}

	if resuming {
		command += " " + harness.QuoteArg(sessionID)

		prompt := launch.Prompt
		if adopting {
			prompt = h.openingPrompt(launch)
		}
		if prompt == "" {
			return command, nil
		}

		path, err := harness.WriteTempText(launch.CacheDir, "codex-prompt-*.md", prompt)
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
