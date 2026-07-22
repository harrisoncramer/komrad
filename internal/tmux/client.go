// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error; side-effect methods are best effort, since the
// panel closes right after and a failure is not recoverable.
package tmux

import (
	"os/exec"
	"strconv"
	"strings"

	"claude-agents/lib/xstr"
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

// PaneLocation returns the "session:window" of a pane, for selecting its window.
func (c *Client) PaneLocation(pane string) string {
	loc, _ := c.query("display-message", "-p", "-t", pane, "#{session_name}:#{window_index}")
	return loc
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

// FirstClient returns the first attached client, empty if none.
func (c *Client) FirstClient() string {
	out, _ := c.query("list-clients", "-F", "#{client_name}")
	return xstr.FirstLine(out)
}

// HasSession reports whether a session with the given name exists.
func (c *Client) HasSession(name string) bool {
	_, err := c.query("has-session", "-t", name)
	return err == nil
}

// SwitchClient points a client at a target session.
func (c *Client) SwitchClient(client, session string) {
	c.run(append(append([]string{"switch-client"}, clientTarget(client)...), "-t", session)...)
}

// SwitchToLast points a client back at its previous session.
func (c *Client) SwitchToLast(client string) {
	c.run(append(append([]string{"switch-client"}, clientTarget(client)...), "-l")...)
}

// SelectWindow focuses a "session:window" location.
func (c *Client) SelectWindow(location string) {
	c.run("select-window", "-t", location)
}

// SelectPane focuses a pane within its window.
func (c *Client) SelectPane(pane string) {
	c.run("select-pane", "-t", pane)
}

// NewDetachedSession creates a detached session rooted at dir, locked to a fixed
// size so its capture always fits the preview box. resize-window sets the size
// and switches the window to manual sizing so it stays put.
func (c *Client) NewDetachedSession(name, dir string, width, height int) {
	c.run("new-session", "-d", "-s", name, "-c", dir)
	c.run("resize-window", "-t", name, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
}

// SendKeys sends literal keys to a target pane or session.
func (c *Client) SendKeys(target string, keys ...string) {
	c.run(append([]string{"send-keys", "-t", target}, keys...)...)
}

// KillSession tears down a session by name.
func (c *Client) KillSession(name string) {
	c.run("kill-session", "-t", name)
}

// RunShellBackground runs a shell command on the server, detached.
func (c *Client) RunShellBackground(command string) {
	c.run("run-shell", "-b", command)
}

// DisplayPopup opens a popup on a client running the given shell command.
func (c *Client) DisplayPopup(client, width, height, command string) {
	c.run("display-popup", "-c", client, "-w", width, "-h", height, "-E", command)
}

// CapturePane returns the visible content of a pane plus recent scrollback,
// with colors preserved.
func (c *Client) CapturePane(pane string) string {
	out, _ := c.query("capture-pane", "-p", "-e", "-t", pane, "-S", "-200")
	return out
}
