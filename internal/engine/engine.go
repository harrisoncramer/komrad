// Package engine says how the panel reaches the komrad an agent is running in.
//
// An agent's terminal belongs to the tmux server the engine runs on. When that is this machine
// the panel shows it by moving the pane itself into the stage with swap-pane, which the rest of
// the layout code assumes. When the engine is somewhere else the panel reaches its api through a
// forwarded port, and the stage streams the agent's terminal over that api.
package engine

import (
	"strings"
)

// Remote is an engine on another machine, the zero value meaning the engine is this machine.
type Remote struct {
	// Destination of the remote.
	Destination string
	// Forward carries a port from this machine to the engine, with LocalPortPlaceholder and
	// RemotePortPlaceholder standing for the two ends.
	Forward []string
}

// IsRemote reports whether the engine is somewhere this panel's tmux cannot reach.
func (r Remote) IsRemote() bool {
	return strings.TrimSpace(r.Destination) != "" || len(r.Forward) > 0
}

// Placeholders a forward's argv may carry, replaced with the two ends of the forward.
const (
	LocalPortPlaceholder  = "{{local_port}}"
	RemotePortPlaceholder = "{{remote_port}}"
)

// ViewSession is the name of the session a stage pane attaches to for one agent's pane.
func ViewSession(pane string) string {
	return "view-" + strings.TrimPrefix(pane, "%")
}

// fill replaces the placeholders in a forward's argv, leaving anything else as written.
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

// String names the engine for a log line or an error.
func (r Remote) String() string {
	if !r.IsRemote() {
		return "local"
	}

	if r.Destination == "" {
		return strings.Join(r.Forward, " ")
	}

	return r.Destination
}
