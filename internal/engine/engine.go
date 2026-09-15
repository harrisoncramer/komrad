// Package engine says how the panel reaches the weave an agent is running in.
//
// Explainer: an agent's terminal belongs to the tmux server the engine runs on. When that is
// this machine the panel shows it by moving the pane itself into the stage, which is what
// swap-pane does and what the rest of the layout code assumes. When the engine is somewhere
// else that move is impossible, because a pane cannot cross tmux servers, so the stage instead
// runs a command that attaches to the engine's tmux over ssh and lets the local server draw
// what comes back.
package engine

import (
	"fmt"
	"strings"
)

// Remote is an engine reached over ssh, the zero value meaning the engine is this machine.
type Remote struct {
	// Destination is what ssh is given, like user@host.
	Destination string
	// Socket is the tmux socket the engine's server runs on.
	Socket string
	// Session is the session every agent is a window of.
	Session string
}

// IsRemote reports whether the engine is somewhere this panel's tmux cannot reach.
func (r Remote) IsRemote() bool { return strings.TrimSpace(r.Destination) != "" }

// ViewSession is the name of the session a stage pane attaches to for one agent's pane.
func ViewSession(pane string) string {
	return "view-" + strings.TrimPrefix(pane, "%")
}

// AttachArgv is what a stage pane runs to show an agent whose terminal is on another machine.
func (r Remote) AttachArgv(pane string) []string {
	if !r.IsRemote() || strings.TrimSpace(pane) == "" {
		return nil
	}

	return []string{"ssh", "-t", r.Destination, r.attachScript(pane)}
}

// attachScript is the shell the far side runs, written to be safe to run again because the
// stage re-runs it whenever the cursor comes back to the same agent.
func (r Remote) attachScript(pane string) string {
	tmux := "tmux -L " + quote(r.Socket)
	view := quote(ViewSession(pane))

	// The window is looked up rather than passed in, since the panel knows an agent by its pane
	// and only the engine can say which window holds it.
	window := "$(" + tmux + " display-message -p -t " + quote(pane) + " '#{window_id}')"

	return strings.Join([]string{
		"set -e",
		"w=" + window,
		tmux + " new-session -d -s " + view + " -t " + quote(r.Session) + " 2>/dev/null || true",
		tmux + " set-option -t " + view + " status off",
		tmux + " select-window -t " + view + ":$w",
		"exec " + tmux + " attach -t " + view,
	}, "; ")
}

// quote wraps a value for the shell on the far side, which is given one string rather than an
// argv and so would otherwise split on whatever the value contains.
func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// String names the engine for a log line or an error.
func (r Remote) String() string {
	if !r.IsRemote() {
		return "local"
	}

	return fmt.Sprintf("%s (tmux -L %s)", r.Destination, r.Socket)
}
