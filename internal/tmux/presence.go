package tmux

import (
	"strconv"
	"strings"
	"time"
)

// IsClientAtPane reports whether somebody is attached to the pane's session, looking at the
// window the pane is in, and has pressed a key there inside the window given.
func (c *Client) IsClientAtPane(pane string, within time.Duration) bool {
	if pane == "" {
		return false
	}

	// A server that will not answer counts as somebody being there.
	out, err := c.query("display-message", "-p", "-t", pane, "#{window_active},#{session_name}")
	if err != nil {
		return true
	}

	active, session, _ := strings.Cut(out, ",")
	if active != "1" || session == "" {
		return false
	}

	listed, err := c.query("list-clients", "-t", session, "-F", "#{client_activity}")
	if err != nil {
		return true
	}

	return isAnyClientRecent(listed, time.Now().Add(-within))
}

// isAnyClientRecent reads the times a session's clients last took a key for one after the bound,
// each of which tmux writes as whole seconds since the epoch.
func isAnyClientRecent(listed string, bound time.Time) bool {
	for line := range strings.SplitSeq(listed, "\n") {
		seconds, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
		if err != nil {
			continue
		}

		if !time.Unix(seconds, 0).Before(bound) {
			return true
		}
	}

	return false
}
