// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error. Side-effect methods are best effort, since the
// panel closes right after and a failure is not recoverable.
package tmux

import (
	"os/exec"
	"strconv"
	"strings"
)

// Client talks to one tmux server socket.
type Client struct {
	socket string
}

// NewClient returns a Client bound to the given tmux server socket.
func NewClient(socket string) *Client {
	return &Client{socket: socket}
}

// Socket returns the tmux server socket this client targets.
func (c *Client) Socket() string {
	return c.socket
}

func (c *Client) query(args ...string) (string, error) {
	out, err := exec.Command("tmux", append([]string{"-L", c.socket}, args...)...).Output()
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

// CurrentSession returns the session the calling pane lives in.
func (c *Client) CurrentSession() (string, error) {
	return c.query("display-message", "-p", "#{session_name}")
}

// PaneID resolves a pane target to its canonical id, empty if it no longer exists.
func (c *Client) PaneID(target string) (string, error) {
	return c.query("display-message", "-p", "-t", target, "#{pane_id}")
}

// PaneAlive reports whether the given pane still exists.
func (c *Client) PaneAlive(pane string) bool {
	if pane == "" {
		return true
	}

	id, err := c.PaneID(pane)
	return err == nil && id == pane
}

// ClientSession returns the session a client is currently viewing.
func (c *Client) ClientSession(client string) string {
	session, _ := c.query(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{session_name}")...)...)
	return session
}

// ClientPath returns a client's active pane working directory.
func (c *Client) ClientPath(client string) string {
	path, _ := c.query(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{pane_current_path}")...)...)
	return path
}

// HasSession reports whether a session with the given name exists.
func (c *Client) HasSession(name string) bool {
	_, err := c.query("has-session", "-t", name)
	return err == nil
}

// SwitchToLast points a client back at its previous session.
func (c *Client) SwitchToLast(client string) {
	c.run(append(append([]string{"switch-client"}, clientTarget(client)...), "-l")...)
}

// NewDetachedSession creates a detached session rooted at dir. Sizing is handled
// per-preview (resize to the box) and restored on attach. The status bar is
// disabled for the session so the agent's chat view fills the popup without the
// plugin's own tmux footer.
func (c *Client) NewDetachedSession(name, dir string) {
	c.run("new-session", "-d", "-s", name, "-c", dir)
	c.run("set-option", "-t", name, "status", "off")
}

// SendKeys sends literal keys to a target pane or session.
func (c *Client) SendKeys(target string, keys ...string) {
	c.run(append([]string{"send-keys", "-t", target}, keys...)...)
}

// CurrentPane returns the id of the pane the calling process runs in. The panel
// uses it as the stable anchor for the stage split.
func (c *Client) CurrentPane() (string, error) {
	return c.query("display-message", "-p", "#{pane_id}")
}

// SplitRight splits the anchor pane horizontally, adding a pane to its right
// rooted at dir, and returns the new pane's id. The split stays detached (-d) so
// focus remains on the anchor. This is the stage the panel drives.
func (c *Client) SplitRight(anchorPane, dir string) string {
	pane, _ := c.query("split-window", "-h", "-d", "-t", anchorPane, "-c", dir, "-P", "-F", "#{pane_id}")
	return pane
}

// NewWindow creates a detached window in a session, rooted at dir and titled
// name, and returns its pane id. Agents live in these background windows; the
// pane id is their stable handle regardless of where the pane is later moved.
func (c *Client) NewWindow(session, name, dir string) string {
	pane, _ := c.query("new-window", "-d", "-t", session, "-n", name, "-c", dir, "-P", "-F", "#{pane_id}")
	return pane
}

// SwapPanes exchanges two panes' positions, keeping the active pane unchanged
// (-d). Pane ids travel with their processes, so this moves a pane into or out of
// the stage slot without disturbing which pane has focus.
func (c *Client) SwapPanes(src, dst string) {
	c.run("swap-pane", "-d", "-s", src, "-t", dst)
}

// RespawnPane replaces a pane's process with the given command (argv), rooted at
// dir. With no command it re-runs the default shell, blanking the pane.
func (c *Client) RespawnPane(pane, dir string, argv ...string) {
	c.run(append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, argv...)...)
}

// KillPane tears down a single pane. When it is the only pane in its window the
// window closes too, but the session (which always holds the panel) survives.
func (c *Client) KillPane(pane string) {
	c.run("kill-pane", "-t", pane)
}

// KillSession tears down a session by name.
func (c *Client) KillSession(name string) {
	c.run("kill-session", "-t", name)
}

// RenameSession renames a session (no-op if it does not exist).
func (c *Client) RenameSession(oldName, newName string) {
	c.run("rename-session", "-t", oldName, newName)
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

// DetachSession detaches every client attached to the named session, leaving the
// session and its pane processes running. It is how the manager popup is hidden:
// the client viewing the persistent manager session drops away while the session
// stays alive, so reopening restores it exactly.
func (c *Client) DetachSession(name string) {
	c.run("detach-client", "-s", name)
}

// CapturePane returns the visible content of a pane with colors preserved.
// Claude runs on the alternate screen, so tmux holds no scrollback for it. The
// visible frame is all there is to capture.
func (c *Client) CapturePane(pane string) string {
	out, _ := c.query("capture-pane", "-p", "-e", "-t", pane)
	return out
}

// SessionAttached reports whether any client is currently viewing a session.
func (c *Client) SessionAttached(session string) bool {
	out, _ := c.query("list-clients", "-t", session, "-F", "#{client_name}")
	return out != ""
}

// ResizeWindow sizes a window (switching it to manual sizing).
func (c *Client) ResizeWindow(target string, width, height int) {
	c.run("resize-window", "-t", target, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
}

// SetWindowSizeLatest returns a window to sizing after the most recent client,
// so it fills the real terminal once attached.
func (c *Client) SetWindowSizeLatest(target string) {
	c.run("set-window-option", "-t", target, "window-size", "latest")
}
