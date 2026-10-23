package engine

import (
	"strings"
)

// Remote is an engine on another machine, the zero value meaning the engine is this machine.
type Remote struct {
	// URL is the https address the engine is served on.
	URL string
}

// IsRemote reports whether the engine is somewhere this panel's tmux cannot reach.
func (r Remote) IsRemote() bool {
	return r.URL != ""
}

// ViewSession is the name of the session a stage pane attaches to for one agent's pane.
func ViewSession(pane string) string {
	return "view-" + strings.TrimPrefix(pane, "%")
}

// String names the engine for a log line or an error.
func (r Remote) String() string {
	if !r.IsRemote() {
		return "local"
	}

	return r.URL
}
