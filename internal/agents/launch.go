package agents

import (
	_ "embed"
	"fmt"
	"strings"

	"weave/internal/appconfig"
	"weave/internal/harness"
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

// briefingDoc is what every spawned agent is told about the panel running it: what it can
// look up rather than guess at, what the panel reads out of its pane, and where to find
// the log when the panel is behaving oddly. It stops at what an agent needs to work under
// weave, since most agents are working on something else entirely and how this repo builds
// itself is no use to them. It is prose in a file rather than a string constant because
// editing it should not mean reading around Go quoting, and it names only the tools an
// agent would not think to go looking for. The rest of the surface is in the tool list its
// client already has.
//
//go:embed briefing.md
var briefingDoc string

// mcpURLPlaceholder is where the briefing names the panel's own endpoint. It is
// substituted rather than formatted, since a %s in prose about a shell command line is
// too easy to write by accident.
const mcpURLPlaceholder = "{{MCP_URL}}"

// logPathPlaceholder is where the briefing names the service's log, resolved the same way
// the service that writes it does, so an agent sent looking for a failure is sent to the
// file that holds it.
const logPathPlaceholder = "{{LOG_PATH}}"

// briefing is the doc with the paths this install actually runs on filled in.
func (l *Launcher) briefing() string {
	text := strings.ReplaceAll(briefingDoc, mcpURLPlaceholder, fmt.Sprintf("http://%s/mcp", l.serverAddr))

	return strings.ReplaceAll(text, logPathPlaceholder, appconfig.LogPath())
}

// appendedSystemPrompt is the standing briefing followed by the workflow's shared
// context, which is what the panel has to say to an agent it is starting.
func (l *Launcher) appendedSystemPrompt(workflowContext string) string {
	briefing := l.briefing()
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
