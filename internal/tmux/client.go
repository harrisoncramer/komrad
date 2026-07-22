// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error; side-effect methods are best effort, since the
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
// per-preview (resize to the box) and restored on attach.
func (c *Client) NewDetachedSession(name, dir string) {
	c.run("new-session", "-d", "-s", name, "-c", dir)
}

// SendKeys sends literal keys to a target pane or session.
func (c *Client) SendKeys(target string, keys ...string) {
	c.run(append([]string{"send-keys", "-t", target}, keys...)...)
}

// KillSession tears down a session by name.
func (c *Client) KillSession(name string) {
	c.run("kill-session", "-t", name)
}

// CapturePane returns the visible content of a pane plus recent scrollback,
// with colors preserved.
func (c *Client) CapturePane(pane string) string {
	out, _ := c.query("capture-pane", "-p", "-e", "-t", pane, "-S", "-200")
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
