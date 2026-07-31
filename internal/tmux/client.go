// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error. Side-effect methods are best effort, since the panel
// closes right after and a failure is not recoverable.
package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// Client talks to one tmux server, addressed either by socket name or by socket path.
// The server flags are held whole rather than the socket alone, since the two forms
// differ in flag as well as value.
type Client struct {
	server []string
}

// NewClient returns a Client bound to a tmux server by socket name, which tmux resolves
// under its own socket directory. The manager's server is reached this way.
func NewClient(socket string) *Client {
	return &Client{server: []string{"-L", socket}}
}

// NewClientAtPath returns a Client bound to a tmux server by socket path. The user's
// server is reached this way, since a popup is told the path of the server that opened
// it and cannot assume the name it was started under.
func NewClientAtPath(path string) *Client {
	return &Client{server: []string{"-S", path}}
}

func (c *Client) query(args ...string) (string, error) {
	out, err := exec.Command("tmux", slices.Concat(c.server, args)...).Output()
	return strings.TrimSpace(string(out)), err
}

func (c *Client) run(args ...string) {
	_, _ = c.query(args...)
}

func clientTarget(client string) []string {
	if client == "" {
		return nil
	}

	return []string{"-c", client}
}

// paneID resolves a pane target to its canonical id, empty if it no longer exists.
func (c *Client) paneID(target string) (string, error) {
	return c.query("display-message", "-p", "-t", target, "#{pane_id}")
}

// PaneAlive reports whether the given pane still exists. It answers for a pane the
// caller created and still holds, such as the panel's own stage. An agent's pane is
// asked about with PaneOwner instead, since a stored id alone is not evidence the pane
// behind it is still that agent's.
func (c *Client) PaneAlive(pane string) bool {
	if pane == "" {
		return false
	}

	id, err := c.paneID(pane)
	return err == nil && id == pane
}

// PaneWidth returns how many columns wide a pane is, zero when the pane is gone or
// tmux answers with something that is not a count. A caller laying content out for a
// pane asks tmux rather than working the width out from the split it requested, since
// the two part company as soon as anything resizes a pane by hand.
func (c *Client) PaneWidth(pane string) int {
	if pane == "" {
		return 0
	}

	out, err := c.query("display-message", "-p", "-t", pane, "#{pane_width}")
	if err != nil {
		return 0
	}

	width, err := strconv.Atoi(out)
	if err != nil {
		return 0
	}

	return width
}

// paneOwnerOption is the pane-scoped tmux option an agent's pane is stamped with.
// The @ prefix is what tmux reserves for user options, so it cannot collide with
// an option tmux itself defines now or later.
const paneOwnerOption = "@claude_agent_id"

// SetPaneOwner stamps a pane with the id of the agent running in it. tmux only promises
// a pane id is unique while the server that issued it lives, so numbering restarts from
// %0 after a restart and an id recorded against a dormant agent is handed out again to
// somebody else's pane. The stamp travels with the pane for its whole life, no matter
// which window or session it is moved into, and a pane from a dead server carries
// nothing.
func (c *Client) SetPaneOwner(pane, agentID string) error {
	if pane == "" || agentID == "" {
		return errors.New("refusing to stamp a pane without both a pane and an agent")
	}

	if _, err := c.query("set-option", "-p", "-t", pane, paneOwnerOption, agentID); err != nil {
		return fmt.Errorf("failed to stamp pane %q for agent %q: %w", pane, agentID, err)
	}

	return nil
}

// PaneOwner returns the id of the agent whose pane this is, empty when the pane is
// gone, was not stamped, or belongs to something other than an agent. A caller
// compares it against the agent it holds rather than treating a non-empty answer as
// liveness.
func (c *Client) PaneOwner(pane string) string {
	if pane == "" {
		return ""
	}

	owner, _ := c.query("show-options", "-p", "-qv", "-t", pane, paneOwnerOption)

	return owner
}

// ClientPath returns a client's active pane working directory.
func (c *Client) ClientPath(client string) string {
	path, _ := c.query(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{pane_current_path}")...)...)
	return path
}

// SendKeys sends literal keys to a target pane or session. An empty target is refused,
// since tmux resolves one to the active pane and a caller holding a pane id that was
// never created would type its command into whatever the user is looking at.
func (c *Client) SendKeys(target string, keys ...string) error {
	if target == "" {
		return errors.New("refusing to send keys to an empty target")
	}

	if _, err := c.query(append([]string{"send-keys", "-t", target}, keys...)...); err != nil {
		return fmt.Errorf("failed to send keys to %q: %w", target, err)
	}

	return nil
}

// CurrentPane returns the id of the pane the calling process runs in, which the panel
// uses as the anchor for the stage split.
func (c *Client) CurrentPane() (string, error) {
	return c.query("display-message", "-p", "#{pane_id}")
}

// SplitRight splits the anchor pane horizontally, adding a pane to its right rooted at
// dir and taking percent of the width, and returns the new pane's id. The split stays
// detached so focus remains on the anchor.
func (c *Client) SplitRight(anchorPane, dir string, percent int) string {
	size := fmt.Sprintf("%d%%", percent)
	pane, _ := c.query("split-window", "-h", "-d", "-t", anchorPane, "-c", dir, "-l", size, "-P", "-F", "#{pane_id}")

	return pane
}

// ResizePane sets a pane's width to percent of the window. tmux clamps it to what
// the window can give both panes, so an extreme share ends up at the nearest one
// that lays out rather than failing.
func (c *Client) ResizePane(pane string, percent int) {
	c.run("resize-pane", "-t", pane, "-x", fmt.Sprintf("%d%%", percent))
}

// ZoomPane zooms a pane to fill its window, or restores the window's layout when zoom
// is false. tmux only offers a toggle, so the window's current state is read first and
// the toggle is sent only when it disagrees with what the caller asked for. That also
// means a window the user zoomed by hand is left as it is rather than being flipped by a
// caller that asked for what is already true.
func (c *Client) ZoomPane(pane string, zoom bool) {
	if pane == "" {
		return
	}

	flag, err := c.query("display-message", "-p", "-t", pane, "#{window_zoomed_flag}")
	if err != nil || (flag == "1") == zoom {
		return
	}

	c.run("resize-pane", "-Z", "-t", pane)
}

// NewWindow creates a detached window in a session, rooted at dir and titled name, and
// returns its pane id. An error is returned rather than an empty id, since every caller
// needs a real pane to drive and tmux resolves an empty target to the active pane.
func (c *Client) NewWindow(session, name, dir string) (string, error) {
	pane, err := c.query("new-window", "-d", "-t", session, "-n", name, "-c", dir, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", fmt.Errorf("failed to create window %q: %w", name, err)
	}
	if pane == "" {
		return "", fmt.Errorf("tmux created no pane for window %q", name)
	}

	return pane, nil
}

// SwapPanes exchanges two panes' positions, keeping the active pane unchanged, which is
// how a pane moves into and out of the stage slot without disturbing focus.
func (c *Client) SwapPanes(src, dst string) {
	c.run("swap-pane", "-d", "-s", src, "-t", dst)
}

// RespawnPane replaces a pane's process with argv, rooted at dir. With no
// command it re-runs the default shell, blanking the pane.
//
// The history is cleared afterwards because tmux pushes the dying process's visible
// screen into the pane's scrollback, and a pager that stays out of the alternate screen
// leaves everything it painted there too. A respawn means the pane's whole content is
// being replaced, so scrolling back into what the last one showed only turns up a stack
// of stale screens. Clearing after the respawn rather than before is what catches the
// screen the kill just pushed, and it costs nothing on-screen, since clear-history
// leaves the visible pane alone.
func (c *Client) RespawnPane(pane, dir string, argv ...string) {
	c.run(append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, argv...)...)
	c.run("clear-history", "-t", pane)
}

// KillPane tears down a single pane. An empty target is refused for the same reason
// SendKeys refuses one, that tmux would kill the active pane instead.
func (c *Client) KillPane(pane string) {
	if pane == "" {
		return
	}

	c.run("kill-pane", "-t", pane)
}

// BindKey binds a key in the prefix table to a tmux command, replacing whatever
// it was bound to.
func (c *Client) BindKey(key string, command ...string) {
	c.run(append([]string{"bind-key", key}, command...)...)
}

// SetGlobal sets a global tmux option.
func (c *Client) SetGlobal(name, value string) {
	c.run("set-option", "-g", name, value)
}

// SetSessionOption sets a session-scoped tmux option.
func (c *Client) SetSessionOption(target, name, value string) {
	c.run("set-option", "-t", target, name, value)
}

// ShowGlobal returns a global tmux option's value, empty if it is unset.
func (c *Client) ShowGlobal(name string) string {
	value, _ := c.query("show-option", "-gqv", name)
	return value
}

// DetachSession detaches every client attached to the named session, leaving the session
// and its pane processes running. It is how the manager popup is hidden, so reopening
// restores it exactly.
func (c *Client) DetachSession(name string) {
	c.run("detach-client", "-s", name)
}

// CapturePane returns the visible content of a pane with colors preserved.
// An agent CLI drawing a TUI runs on the alternate screen, so tmux holds no
// scrollback for it. The visible frame is all there is to capture.
func (c *Client) CapturePane(pane string) string {
	out, _ := c.query("capture-pane", "-p", "-e", "-t", pane)
	return out
}
