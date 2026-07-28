// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error. Side-effect methods are best effort, since the panel
// closes right after and a failure is not recoverable.
package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Client talks to one tmux server, addressed either by socket name or by socket
// path. The server flags are held whole rather than the socket alone, since the
// two forms differ in flag as well as value.
type Client struct {
	server []string
}

// NewClient returns a Client bound to a tmux server by socket name, which tmux
// resolves under its own socket directory. The manager's server is reached this
// way, since its name is configuration.
func NewClient(socket string) *Client {
	return &Client{server: []string{"-L", socket}}
}

// NewClientAtPath returns a Client bound to a tmux server by socket path. The
// user's server is reached this way, since a popup is told the path of the server
// that opened it and cannot assume the name it was started under.
func NewClientAtPath(path string) *Client {
	return &Client{server: []string{"-S", path}}
}

func (c *Client) query(args ...string) (string, error) {
	out, err := exec.Command("tmux", append(append([]string{}, c.server...), args...)...).Output()
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

// PaneAlive reports whether the given pane still exists.
func (c *Client) PaneAlive(pane string) bool {
	if pane == "" {
		return false
	}

	id, err := c.paneID(pane)
	return err == nil && id == pane
}

// ClientPath returns a client's active pane working directory.
func (c *Client) ClientPath(client string) string {
	path, _ := c.query(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{pane_current_path}")...)...)
	return path
}

// SendKeys sends literal keys to a target pane or session. An empty target is
// refused, since tmux resolves one to the active pane and a caller holding a pane
// id that was never created would type its command into whatever the user is
// looking at.
func (c *Client) SendKeys(target string, keys ...string) error {
	if target == "" {
		return errors.New("refusing to send keys to an empty target")
	}

	if _, err := c.query(append([]string{"send-keys", "-t", target}, keys...)...); err != nil {
		return fmt.Errorf("failed to send keys to %q: %w", target, err)
	}

	return nil
}

// CurrentPane returns the id of the pane the calling process runs in. The panel
// uses it as the stable anchor for the stage split.
func (c *Client) CurrentPane() (string, error) {
	return c.query("display-message", "-p", "#{pane_id}")
}

// SplitRight splits the anchor pane horizontally, adding a pane to its right
// rooted at dir, and returns the new pane's id. The split stays detached so
// focus remains on the anchor. This is the stage the panel drives.
func (c *Client) SplitRight(anchorPane, dir string) string {
	pane, _ := c.query("split-window", "-h", "-d", "-t", anchorPane, "-c", dir, "-P", "-F", "#{pane_id}")
	return pane
}

// NewWindow creates a detached window in a session, rooted at dir and titled name,
// and returns its pane id. Agents live in these background windows, and the pane id
// is their stable handle regardless of where the pane is later moved. An error is
// returned rather than an empty id, since every caller needs a real pane to drive
// and tmux resolves an empty target to the active pane.
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

// SwapPanes exchanges two panes' positions, keeping the active pane unchanged.
// Pane ids travel with their processes, so this moves a pane into or out of the
// stage slot without disturbing which pane has focus.
func (c *Client) SwapPanes(src, dst string) {
	c.run("swap-pane", "-d", "-s", src, "-t", dst)
}

// RespawnPane replaces a pane's process with argv, rooted at dir. With no
// command it re-runs the default shell, blanking the pane.
func (c *Client) RespawnPane(pane, dir string, argv ...string) {
	c.run(append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, argv...)...)
}

// KillPane tears down a single pane. An empty target is refused for the same reason
// SendKeys refuses one, that tmux would kill the active pane instead. When the pane
// is the only one in its window the window closes too, while the session holding
// the panel survives.
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

// DetachSession detaches every client attached to the named session, leaving the
// session and its pane processes running. It is how the manager popup is hidden.
// The client viewing the persistent manager session drops away while the session
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
