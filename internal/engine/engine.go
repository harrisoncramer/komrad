// Package engine says how the panel reaches the komrad an agent is running in.
//
// An agent's terminal belongs to the tmux server the engine runs on. When that is this machine
// the panel shows it by moving the pane itself into the stage with swap-pane, which the rest of
// the layout code assumes. When the engine is somewhere else the pane cannot be moved, so the
// stage instead runs a command that attaches to the engine's tmux over ssh and lets the local
// server draw what comes back.
package engine

import (
	"fmt"
	"strings"
)

// Remote is an engine on another machine, the zero value meaning the engine is this machine.
type Remote struct {
	// Destination of the remote.
	Destination string
	// Exec runs a command on the engine, with CommandPlaceholder standing for that command.
	Exec []string
	// Forward carries a port from this machine to the engine, with LocalPortPlaceholder and
	// RemotePortPlaceholder standing for the two ends.
	Forward []string
	// User is the account on the engine that owns its tmux server.
	User string
	// Socket is the tmux socket the engine's server runs on.
	Socket string
	// Session is the session every agent is a window of.
	Session string
}

// IsRemote reports whether the engine is somewhere this panel's tmux cannot reach.
func (r Remote) IsRemote() bool {
	return strings.TrimSpace(r.Destination) != "" || len(r.Exec) > 0
}

// Placeholders a transport's argv may carry, replaced with what komrad needs it to carry.
const (
	CommandPlaceholder     = "{{command}}"
	CommandJSONPlaceholder = "{{command_json}}"
	LocalPortPlaceholder   = "{{local_port}}"
	RemotePortPlaceholder  = "{{remote_port}}"
)

// ViewSession is the name of the session a stage pane attaches to for one agent's pane.
func ViewSession(pane string) string {
	return "view-" + strings.TrimPrefix(pane, "%")
}

// AttachArgv is what a stage pane runs to show an agent whose terminal is on another machine.
func (r Remote) AttachArgv(pane string) []string {
	if !r.IsRemote() || strings.TrimSpace(pane) == "" {
		return nil
	}

	return r.argv(r.attachScript(pane))
}

func (r Remote) argv(command string) []string {
	// A transport that does not arrive as the account owning the engine's tmux steps into it
	// here.
	if r.User != "" {
		command = "sudo -u " + Quote(r.User) + " sh -c " + Quote(command)
	}

	if len(r.Exec) > 0 {
		return fill(r.Exec, map[string]string{
			CommandPlaceholder:     command,
			CommandJSONPlaceholder: escapeJSON(command),
		})
	}

	return []string{"ssh", "-t", r.Destination, command}
}

// fill replaces the placeholders in a transport's argv, leaving anything else as written.
func fill(argv []string, values map[string]string) []string {
	filled := make([]string, 0, len(argv))

	for _, word := range argv {
		for placeholder, value := range values {
			word = strings.ReplaceAll(word, placeholder, value)
		}

		filled = append(filled, word)
	}

	return filled
}

// escapeJSON makes a command safe to place inside the json the ssm parameters are given as.
func escapeJSON(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)

	return strings.ReplaceAll(value, `"`, `\"`)
}

// attachScript is the shell the far side runs, written to be safe to run again because the
// stage re-runs it whenever the cursor comes back to the same agent.
func (r Remote) attachScript(pane string) string {
	tmux := "tmux -L " + Quote(r.Socket)
	view := Quote(ViewSession(pane))

	// The window is looked up rather than passed in.
	window := "$(" + tmux + " display-message -p -t " + Quote(pane) + " '#{window_id}')"

	return strings.Join([]string{
		"set -e",
		"w=" + window,
		tmux + " new-session -d -s " + view + " -t " + Quote(r.Session) + " 2>/dev/null || true",
		tmux + " set-option -t " + view + " status off",
		tmux + " select-window -t " + view + ":$w",
		"exec " + tmux + " attach -d -t " + view,
	}, "; ")
}

// Quote wraps a value for the shell on the far side, which is given one string rather than an
// argv.
func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// String names the engine for a log line or an error.
func (r Remote) String() string {
	if !r.IsRemote() {
		return "local"
	}

	if r.Destination == "" {
		return fmt.Sprintf("%s (tmux -L %s)", strings.Join(r.Exec, " "), r.Socket)
	}

	return fmt.Sprintf("%s (tmux -L %s)", r.Destination, r.Socket)
}
