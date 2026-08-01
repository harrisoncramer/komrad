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

// Client talks to one tmux server, addressed by socket name or socket path. The server
// flags are held whole, since the two forms differ in flag as well as value.
type Client struct {
	server []string
}

// NewClient returns a Client bound to a tmux server by socket name, which is how the
// manager's own server is reached.
func NewClient(socket string) *Client {
	return &Client{server: []string{"-L", socket}}
}

// NewClientAtPath returns a Client bound to a tmux server by socket path. The user's
// server is reached this way, since a popup is told the path of the server that opened it
// and cannot assume the name it was started under.
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

// PaneAlive reports whether the given pane still exists, for a pane the caller created
// and still holds such as the panel's own stage. An agent's pane is asked about with
// PaneOwner instead, since a stored id alone is not evidence the pane is still its.
func (c *Client) PaneAlive(pane string) bool {
	if pane == "" {
		return false
	}

	id, err := c.paneID(pane)
	return err == nil && id == pane
}

// PaneWidth returns how many columns wide a pane is, zero when it is gone. tmux is asked
// rather than the width worked out from the requested split, since the two part company
// as soon as anything resizes a pane by hand.
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

// paneOwnerOption is the pane-scoped tmux option an agent's pane is stamped with. The @
// prefix is what tmux reserves for user options, so it cannot collide with its own.
const paneOwnerOption = "@weave_agent_id"

// SetPaneOwner stamps a pane with the id of the agent running in it. tmux only promises a
// pane id is unique while the server that issued it lives, so a dormant agent's recorded
// id is handed out again after a restart. The stamp travels with the pane wherever it is
// moved, and a pane from a dead server carries nothing.
func (c *Client) SetPaneOwner(pane, agentID string) error {
	if pane == "" || agentID == "" {
		return errors.New("refusing to stamp a pane without both a pane and an agent")
	}

	if _, err := c.query("set-option", "-p", "-t", pane, paneOwnerOption, agentID); err != nil {
		return fmt.Errorf("failed to stamp pane %q for agent %q: %w", pane, agentID, err)
	}

	return nil
}

// PaneOwner returns the id of the agent whose pane this is, empty when the pane is gone
// or was never stamped. A caller compares it against the agent it holds rather than
// treating a non-empty answer as liveness.
func (c *Client) PaneOwner(pane string) string {
	if pane == "" {
		return ""
	}

	owner, _ := c.query("show-options", "-p", "-qv", "-t", pane, paneOwnerOption)

	return owner
}

// PaneOwners returns every pane on the server keyed to its owner stamp. The error is
// reported rather than swallowed, since a caller must not read tmux failing to answer as
// every pane being gone.
func (c *Client) PaneOwners() (map[string]string, error) {
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

// ClientPath returns a client's active pane working directory.
func (c *Client) ClientPath(client string) string {
	path, _ := c.query(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{pane_current_path}")...)...)
	return path
}

// SessionClient returns a client attached to the given session, empty when none is. The
// first is taken when several are, since they all show the same session.
func (c *Client) SessionClient(session string) string {
	clients, err := c.query("list-clients", "-t", session, "-F", "#{client_name}")
	if err != nil || clients == "" {
		return ""
	}

	first, _, _ := strings.Cut(clients, "\n")

	return first
}

// ClientSession returns the session a client is attached to, empty when tmux knows no
// such client.
func (c *Client) ClientSession(client string) string {
	session, _ := c.query(append([]string{"display-message", "-p"}, append(clientTarget(client), "#{client_session}")...)...)
	return session
}

// OpenWindow creates a window in the session the given client is attached to, rooted at
// dir and running argv, and leaves it current. It is how something is put in front of the
// user on their own tmux server, where the client viewing the popup is the only handle on
// a session, so the window is created attached rather than detached as NewWindow does.
//
// The trailing colon on the session target is what NewWindow explains. An empty client is
// refused for the reason SendKeys refuses an empty target: tmux would resolve it to
// whatever the server holds as current and open the window over a session nobody named.
func (c *Client) OpenWindow(client, name, dir string, argv ...string) error {
	if client == "" {
		return errors.New("refusing to open a window without a client to open it for")
	}

	session := c.ClientSession(client)
	if session == "" {
		return fmt.Errorf("tmux knows no client %q", client)
	}

	args := append([]string{"new-window", "-t", session + ":", "-n", name, "-c", dir}, argv...)
	if _, err := c.query(args...); err != nil {
		return fmt.Errorf("failed to open window %q: %w", name, err)
	}

	return nil
}

// SendKeys sends literal keys to a target pane or session. An empty target is refused,
// since tmux resolves one to the active pane and would type the command into whatever the
// user is looking at.
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

// ZoomPane zooms a pane to fill its window, or restores the window's layout when zoom is
// false. tmux only offers a toggle, so the current state is read first and the toggle sent
// only when it disagrees, which also leaves a window the user zoomed by hand alone.
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
// returns its pane id. An error is returned rather than an empty id, since tmux resolves
// an empty target to the active pane.
//
// The trailing colon is how tmux is told a target is a session and nothing else. A bare
// name matches window names too and prefers those: the manager's window shares its
// session's name, so tmux would insist on creating at that window's index, which is taken.
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
//
// The history is cleared afterwards because tmux pushes the dying process's visible screen
// into the scrollback, so scrolling back would only turn up a stack of stale screens.
// Clearing after rather than before is what catches the screen the kill just pushed, and
// clear-history leaves the visible pane alone.
func (c *Client) RespawnPane(pane, dir string, argv ...string) {
	c.run(append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir}, argv...)...)
	c.run("clear-history", "-t", pane)
}

// SiblingPanes are the other panes sharing a window with the given one, in tmux's order.
// It answers empty for a pane that has gone, since a window nothing is left in has no
// siblings to report.
func (c *Client) SiblingPanes(pane string) []string {
	if pane == "" {
		return nil
	}

	listed, err := c.query("list-panes", "-t", pane, "-F", "#{pane_id}")
	if err != nil || listed == "" {
		return nil
	}

	var siblings []string
	for _, id := range strings.Split(listed, "\n") {
		if id != "" && id != pane {
			siblings = append(siblings, id)
		}
	}

	return siblings
}

// BreakPane moves a pane into a window of its own, leaving the current window focused.
// Pane ids travel with the pane, so whoever recorded this one still finds it afterwards.
func (c *Client) BreakPane(pane string) {
	if pane == "" {
		return
	}

	c.run("break-pane", "-d", "-s", pane)
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
	out, _ := c.query("capture-pane", "-p", "-e", "-t", pane)
	return out
}
