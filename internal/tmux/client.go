// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error. Side-effect methods are best effort, since the panel
// closes right after and a failure is not recoverable.
package tmux

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"weave/internal/logging"
)

// paneOwnerOption is the pane-scoped tmux option an agent's pane is stamped with. The @
// prefix is what tmux reserves for user options, so it cannot collide with its own.
const paneOwnerOption = "@weave_agent_id"

// Client talks to one tmux server, addressed by socket name or socket path. The server
// flags are held whole, since the two forms differ in flag as well as value.
type Client struct {
	server []string
}

// NewClient returns a Client bound to a tmux server by socket name, which is how the manager's own server is reached.
func NewClient(socket string) *Client {
	return &Client{server: []string{"-L", socket}} // The -L passes commands to a specific socket
}

// NewClientAtPath returns a Client bound to a tmux server by socket path.
func NewClientAtPath(path string) *Client {
	return &Client{server: []string{"-S", path}}
}

// IsPaneAlive reports whether the given pane still exists.
func (c *Client) IsPaneAlive(pane string) bool {
	if pane == "" {
		return false
	}

	id, err := c.paneID(pane)
	return err == nil && id == pane
}

// GetPaneWidth returns how many columns wide a pane is, zero when it is gone.
func (c *Client) GetPaneWidth(pane string) int {
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

// SetPaneOwner stamps a pane with the id of the agent running in it.
func (c *Client) SetPaneOwner(pane, agentID string) error {
	if pane == "" || agentID == "" {
		return errors.New("refusing to stamp a pane without both a pane and an agent")
	}

	if _, err := c.query("set-option", "-p", "-t", pane, paneOwnerOption, agentID); err != nil {
		return fmt.Errorf("failed to stamp pane %q for agent %q: %w", pane, agentID, err)
	}

	return nil
}

// GetPaneOwner returns the id of the agent whose pane this is.
func (c *Client) GetPaneOwner(pane string) string {
	if pane == "" {
		return ""
	}

	return c.value("show-options", "-p", "-qv", "-t", pane, paneOwnerOption)
}

// GetPaneOwners returns every pane on the server keyed to its owner stamp.
func (c *Client) GetPaneOwners() (map[string]string, error) {
	out, err := c.query("list-panes", "-a", "-F", "#{pane_id} #{"+paneOwnerOption+"}")
	if err != nil {
		return nil, fmt.Errorf("failed to list pane owners: %w", err)
	}

	owners := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		pane, owner, _ := strings.Cut(line, " ")
		if pane != "" {
			owners[pane] = owner
		}
	}

	return owners, nil
}

// GetClientPath returns a client's active pane working directory.
func (c *Client) GetClientPath(client string) string {
	return c.value(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{pane_current_path}")...)...)
}

// GetSessionClient returns a client attached to the given session, empty when none is.
func (c *Client) GetSessionClient(session string) string {
	clients, err := c.query("list-clients", "-t", session, "-F", "#{client_name}")
	if err != nil || clients == "" {
		return ""
	}

	first, _, _ := strings.Cut(clients, "\n")

	return first
}

// GetClientSession returns the session a client is attached to, empty when tmux knows no such client.
func (c *Client) GetClientSession(client string) string {
	return c.value(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{client_session}")...)...)
}

// OpenWindow creates a window in the session the given client is attached to, rooted at
// dir and running argv, and leaves it current. It is how something is put in front of the
// user on their own tmux server, where the client viewing the popup is the only handle on
// a session, so the window is created attached rather than detached as NewWindow does.
func (c *Client) OpenWindow(client, name, dir string, argv ...string) error {
	if client == "" {
		return errors.New("refusing to open a window without a client to open it for")
	}

	session := c.GetClientSession(client)
	if session == "" {
		return fmt.Errorf("tmux knows no client %q", client)
	}

	args := append([]string{"new-window", "-t", session + ":", "-n", name, "-c", dir}, argv...)
	if _, err := c.query(args...); err != nil {
		return fmt.Errorf("failed to open window %q: %w", name, err)
	}

	return nil
}

// SendKeys sends literal keys to a target pane or session.
func (c *Client) SendKeys(target string, keys ...string) error {
	if target == "" {
		return errors.New("refusing to send keys to an empty target")
	}

	if _, err := c.query(append([]string{"send-keys", "-t", target}, keys...)...); err != nil {
		return fmt.Errorf("failed to send keys to %q: %w", target, err)
	}

	return nil
}

// SplitRight splits the anchor pane horizontally.
func (c *Client) SplitRight(anchorPane, dir string, percent int) string {
	size := fmt.Sprintf("%d%%", percent)
	return c.value("split-window", "-h", "-d", "-t", anchorPane, "-c", dir, "-l", size, "-P", "-F", "#{pane_id}")
}

// SplitBelow splits a shell under the anchor pane and moves focus into it, rooted at dir.
// It is the panel's terminal key, so the pane is the user's from here: nothing tracks it,
// and it lives until they close it or the next panel start clears the window.
func (c *Client) SplitBelow(anchorPane, dir string) {
	c.run("split-window", "-v", "-t", anchorPane, "-c", dir)
}

// ResizePane sets a pane's width to percent of the window.
func (c *Client) ResizePane(pane string, percent int) {
	c.run("resize-pane", "-t", pane, "-x", fmt.Sprintf("%d%%", percent))
}

// ZoomPane zooms a pane to fill its window, or restores the window's layout when zoom is false.
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
// returns its pane id.
func (c *Client) NewWindow(session, name, dir string) (string, error) {
	pane, err := c.query("new-window", "-d", "-t", session+":", "-n", name, "-c", dir, "-P", "-F", "#{pane_id}")
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

// RespawnPane replaces a pane's process with argv, rooted at dir. With no command it
// re-runs the default shell, blanking the pane.
func (c *Client) RespawnPane(pane, dir string, argv ...string) {
	c.run(append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, argv...)...)
	c.run("clear-history", "-t", pane)
}

// GetSiblingPanes gets the other panes sharing a window with the given one.
func (c *Client) GetSiblingPanes(pane string) []string {
	if pane == "" {
		return nil
	}

	listed, err := c.query("list-panes", "-t", pane, "-F", "#{pane_id}")
	if err != nil || listed == "" {
		return nil
	}

	var siblings []string
	for id := range strings.SplitSeq(listed, "\n") {
		if id != "" && id != pane {
			siblings = append(siblings, id)
		}
	}

	return siblings
}

// BreakPane moves a pane into a window of its own, leaving the current window focused.
func (c *Client) BreakPane(pane string) {
	if pane == "" {
		return
	}

	c.run("break-pane", "-d", "-s", pane)
}

// KillPane tears down a single pane.
func (c *Client) KillPane(pane string) {
	if pane == "" {
		return
	}

	c.run("kill-pane", "-t", pane)
}

// BindKey binds a key in the prefix table to a tmux command, replacing whatever it was bound to.
func (c *Client) BindKey(key string, command ...string) {
	c.run(append([]string{"bind-key", key}, command...)...)
}

// SetGlobal sets a global tmux option.
func (c *Client) SetGlobal(name, value string) {
	c.run("set-option", "-g", name, value)
}

// ShowGlobal returns a global tmux option's value, empty if it is unset.
func (c *Client) ShowGlobal(name string) string {
	return c.value("show-option", "-gqv", name)
}

// SetSessionOption sets a session-scoped tmux option.
func (c *Client) SetSessionOption(target, name, value string) {
	c.run("set-option", "-t", target, name, value)
}

// DefaultShell returns the shell tmux starts a pane's process with, which is the only
// option that decides what a pane can parse, since tmux runs a non-empty default-command
// through this shell too.
func (c *Client) DefaultShell() string {
	return c.ShowGlobal("default-shell")
}

// DetachSession detaches every client attached to the named session, leaving its pane
// processes running. It is how the manager popup is hidden, so reopening restores it.
func (c *Client) DetachSession(name string) {
	c.run("detach-client", "-s", name)
}

// CapturePane returns the visible content of a pane with colors preserved. An agent CLI
// drawing a TUI runs on the alternate screen, so the visible frame is all there is.
func (c *Client) CapturePane(pane string) string {
	return c.value("capture-pane", "-p", "-e", "-t", pane)
}

// query runs a tmux command and returns its stdout. A failure carries the command that
// failed and whatever tmux wrote to stderr, since an exit status on its own says neither
// which call broke nor why, and these run on a loop where nobody is watching.
func (c *Client) query(args ...string) (string, error) {
	out, err := exec.Command("tmux", slices.Concat(c.server, args)...).Output()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("tmux %s: %w%s",
			strings.Join(slices.Concat(c.server, args), " "), err, stderrDetail(err))
	}

	return strings.TrimSpace(string(out)), nil
}

// stderrDetail renders what a failed command wrote to stderr, blank when it wrote
// nothing. Output collects it on the ExitError rather than in the error's own text.
func stderrDetail(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ""
	}

	detail := strings.TrimSpace(string(exitErr.Stderr))
	if detail == "" {
		return ""
	}

	return ": " + detail
}

// run performs a side-effecting tmux command. The result stays unreturned, since the
// caller has nothing to do about it, but a failure is logged rather than dropped: silently
// this is a pane that did not die or a key that did not bind, with nothing to read after.
func (c *Client) run(args ...string) {
	if _, err := c.query(args...); err != nil {
		slog.Default().Warn("a tmux command failed", logging.Attrs(logging.Err(err))...)
	}
}

// value runs a tmux command whose answer is a single string, returning empty when it
// fails. The zero value is the answer these callers want, since an option that is unset
// and a pane that is gone both read as nothing, but the failure is logged at debug so a
// wrong answer can still be traced. It is debug rather than warn because these run per
// agent per pass, and a tmux that is down would otherwise fill the log.
func (c *Client) value(args ...string) string {
	out, err := c.query(args...)
	if err != nil {
		slog.Default().Debug("a tmux query failed", logging.Attrs(logging.Err(err))...)
	}

	return out
}

func clientTarget(client string) []string {
	if client == "" {
		return nil
	}

	return []string{"-c", client}
}

// paneID gets a pane target's canonical id, empty if it no longer exists.
func (c *Client) paneID(target string) (string, error) {
	return c.query("display-message", "-p", "-t", target, "#{pane_id}")
}
