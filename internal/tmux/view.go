package tmux

import (
	"fmt"
	"slices"
	"strconv"
)

// OpenView makes view a session sharing session's windows and looking at pane's window, reusing
// view when it is already there.
func (c *Client) OpenView(view, session, pane string) error {
	window, err := c.query("display-message", "-p", "-t", pane, "#{window_id}")
	if err != nil {
		return fmt.Errorf("failed to find the window of pane %s: %w", pane, err)
	}

	if _, err := c.query("new-session", "-d", "-s", view, "-t", "="+session); err != nil {
		if _, hasErr := c.query("has-session", "-t", "="+view); hasErr != nil {
			return fmt.Errorf("failed to open view %s: %w", view, err)
		}
	}

	if _, err := c.query("set-option", "-t", "="+view+":", "status", "off"); err != nil {
		return fmt.Errorf("failed to hide the status line of view %s: %w", view, err)
	}

	if _, err := c.query("select-window", "-t", "="+view+":"+window); err != nil {
		return fmt.Errorf("failed to point view %s at pane %s: %w", view, pane, err)
	}

	return nil
}

// CountSessionClients returns how many clients are attached to a session.
func (c *Client) CountSessionClients(session string) (int, error) {
	out, err := c.query("display-message", "-p", "-t", "="+session+":", "#{session_attached}")
	if err != nil {
		return 0, fmt.Errorf("failed to count the clients of session %s: %w", session, err)
	}

	count, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("failed to count the clients of session %s: %w", session, err)
	}

	return count, nil
}

// KillSession ends a session, leaving any window another session shares.
func (c *Client) KillSession(session string) error {
	if _, err := c.query("kill-session", "-t", "="+session); err != nil {
		return fmt.Errorf("failed to kill session %s: %w", session, err)
	}

	return nil
}

// AttachArgv is the command that attaches a terminal to a session, detaching any other client.
func (c *Client) AttachArgv(session string) []string {
	return slices.Concat([]string{"tmux"}, c.server, []string{"attach-session", "-d", "-t", "=" + session})
}
