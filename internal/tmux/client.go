// Package tmux wraps the tmux CLI for a single tmux server socket. Query methods
// return output and an error. Side-effect methods are best effort, since the panel
// closes right after and a failure is not recoverable.
package tmux

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"weave/internal/logging"
)

// paneOwnerOption is the pane-scoped tmux option an agent's pane is stamped with. The @
// prefix is what tmux reserves for user options, so it cannot collide with its own.
const paneOwnerOption = "@weave_agent_id"

// queryTimeout bounds a single tmux call. Every command here is a request and an answer
// over a local socket, so a healthy one costs milliseconds and only a server that has
// stopped answering trips this.
const queryTimeout = 5 * time.Second

// killDelay is how long a timed-out tmux gets after being killed before its output is
// abandoned. Without it a daemonized server still holding the inherited stdout keeps the
// read blocked, which is the hang the timeout was there to prevent.
const killDelay = 100 * time.Millisecond

// Client talks to one tmux server, addressed by socket name or socket path. The server
// flags are held whole, since the two forms differ in flag as well as value.
type Client struct {
	server []string
}

// DefaultSocket is the socket tmux uses when nothing names one, so it is the server a
// command run from a shell outside tmux means.
const DefaultSocket = "default"

// NewClient returns a Client bound to a tmux server by socket name, which is how the manager's own server is reached.
func NewClient(socket string) *Client {
	return &Client{server: []string{"-L", socket}}
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

// IsPaneInMode reports whether a pane is in one of tmux's own modes, which is what scrolling
// back through it puts it in. Keys sent to a pane in a mode reach the mode rather than the
// program, so this says whether the pane can be typed at.
func (c *Client) IsPaneInMode(pane string) bool {
	if pane == "" {
		return false
	}

	return c.value("display-message", "-p", "-t", pane, "#{pane_in_mode}") == "1"
}

// ClearPaneOwner takes weave's stamp off a pane, which is what a pane weave no longer
// tracks should look like to anything reading the server.
func (c *Client) ClearPaneOwner(pane string) error {
	if pane == "" {
		return errors.New("refusing to unstamp an empty pane")
	}

	if _, err := c.query("set-option", "-p", "-u", "-t", pane, paneOwnerOption); err != nil {
		return fmt.Errorf("failed to unstamp pane %q: %w", pane, err)
	}

	return nil
}

// PaneStatus is what one pane says about itself: the agent it is stamped for, and the
// program in its foreground.
type PaneStatus struct {
	Owner   string
	Command string
}

// shellCommands are the foreground programs that mean a pane holds nothing but the shell
// weave typed the launch command into. The CLIs report anything but their own name here,
// Claude its version string and Codex its wrapper script, so only a recognised shell is
// read as a pane whose CLI has gone and everything else is taken for one still running.
var shellCommands = map[string]bool{
	"sh": true, "-sh": true,
	"bash": true, "-bash": true,
	"zsh": true, "-zsh": true,
	"fish": true, "-fish": true,
	"nu": true, "-nu": true,
}

// RunsShell reports whether the pane's foreground program is a plain shell.
func (p PaneStatus) RunsShell() bool { return shellCommands[p.Command] }

// GetPanes returns every pane on the server keyed to what it says about itself.
func (c *Client) GetPanes() (map[string]PaneStatus, error) {
	out, err := c.query("list-panes", "-a", "-F", "#{pane_id} #{"+paneOwnerOption+"} #{pane_current_command}")
	if err != nil {
		return nil, fmt.Errorf("failed to list panes: %w", err)
	}

	panes := map[string]PaneStatus{}
	for line := range strings.SplitSeq(out, "\n") {
		pane, rest, _ := strings.Cut(line, " ")
		owner, command, _ := strings.Cut(rest, " ")
		if pane != "" {
			panes[pane] = PaneStatus{Owner: owner, Command: command}
		}
	}

	return panes, nil
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

// SendText types a string into a pane as text. SendKeys looks each argument up as a key name
// first, so a word naming a key arrives as that key, and -l is what turns the lookup off. It
// does not rescue a pane in a mode, which reads literal keys through the mode's table too.
func (c *Client) SendText(target, text string) error {
	if target == "" {
		return errors.New("refusing to send text to an empty target")
	}

	if _, err := c.query("send-keys", "-t", target, "-l", "--", text); err != nil {
		return fmt.Errorf("failed to send text to %q: %w", target, err)
	}

	return nil
}

// SplitRight splits a pane right of the anchor, rooted at dir and sized as tmux takes it,
// either a count of cells or a percentage such as "40%". Focus stays where it was, since what
// goes in the pane is decided after it exists and whoever put it there says whether to move.
func (c *Client) SplitRight(anchorPane, dir, size string) string {
	return c.split(anchorPane, dir, size, "-h")
}

// SplitLeft splits a pane left of the anchor.
func (c *Client) SplitLeft(anchorPane, dir, size string) string {
	return c.split(anchorPane, dir, size, "-h", "-b")
}

// SplitBelow splits a pane under the anchor.
func (c *Client) SplitBelow(anchorPane, dir, size string) string {
	return c.split(anchorPane, dir, size, "-v")
}

// SplitAbove splits a pane over the anchor.
func (c *Client) SplitAbove(anchorPane, dir, size string) string {
	return c.split(anchorPane, dir, size, "-v", "-b")
}

func (c *Client) split(anchorPane, dir, size string, flags ...string) string {
	argv := append([]string{"split-window"}, flags...)
	argv = append(argv, "-d", "-t", anchorPane, "-c", dir, "-l", size, "-P", "-F", "#{pane_id}")

	return c.value(argv...)
}

// ResizePane sets a pane's width, as a count of columns or a percentage of the window.
func (c *Client) ResizePane(pane, width string) {
	c.run("resize-pane", "-t", pane, "-x", width)
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

func (c *Client) WaitFor(ctx context.Context, channel string) error {
	command := slices.Concat(c.server, []string{"wait-for", channel})

	cmd := exec.CommandContext(ctx, "tmux", command...)
	cmd.WaitDelay = killDelay

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("tmux %s: %w%s", strings.Join(command, " "), err, stderrDetail(err))
	}

	return nil
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

// BindKeyChecked binds a key like BindKey and says why tmux refused. The panel binds keys on
// its way past, where nothing can be done about a failure, while a person who typed a command
// to bind one is reading its output and has to be told when the server it named is not there.
func (c *Client) BindKeyChecked(key string, command ...string) error {
	if _, err := c.query(append([]string{"bind-key", key}, command...)...); err != nil {
		return fmt.Errorf("failed to bind %q: %w", key, err)
	}

	return nil
}

// SetGlobal sets a global tmux option.
func (c *Client) SetGlobal(name, value string) {
	c.run("set-option", "-g", name, value)
}

// SetGlobalChecked sets a global tmux option and says why tmux refused, for the same reason
// BindKeyChecked does.
func (c *Client) SetGlobalChecked(name, value string) error {
	if _, err := c.query("set-option", "-g", name, value); err != nil {
		return fmt.Errorf("failed to set %s: %w", name, err)
	}

	return nil
}

// SocketPath is the path of the socket this client's server listens on, and an error when no
// server is listening there. It is how a socket path read out of TMUX is told apart from the
// server weave runs the panel on, which is otherwise the same server under two spellings.
func (c *Client) SocketPath() (string, error) {
	path, err := c.query("display-message", "-p", "#{socket_path}")
	if err != nil {
		return "", err
	}

	return path, nil
}

// ShowGlobal returns a global tmux option's value, empty if it is unset.
func (c *Client) ShowGlobal(name string) string {
	return c.value("show-option", "-gqv", name)
}

// SetInfoPane records which pane the panel opened its info region in, and clears it when
// given nothing, so the panel and the service agree on it through tmux rather than through
// each other. It is written on the server the panel's panes live on, not the user's, where
// Servers otherwise sends SetGlobal.
func (c *Client) SetInfoPane(pane string) {
	c.SetGlobal(InfoPaneOption, pane)
}

// GetInfoPane reads that pane back, from the same server it was written on.
func (c *Client) GetInfoPane() string {
	return c.ShowGlobal(InfoPaneOption)
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
//
// Nobody watching is also why the call is bounded. A reconcile pass runs to completion
// before its loop looks at its context again, so a tmux that never answers would stop
// every agent's state being read from then on. Giving up reads as not having read rather
// than as the pane being gone, which is what the callers already do with a failure.
func (c *Client) query(args ...string) (string, error) {
	command := slices.Concat(c.server, args)

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", command...)
	cmd.WaitDelay = killDelay

	out, err := cmd.Output()

	// Running out of time is reported ahead of the error the kill produced, which says only
	// that something signalled the process. Whatever tmux wrote before it went is dropped,
	// since a half-read frame answered as if it were whole is worse than no answer.
	if err != nil && ctx.Err() != nil {
		return "", fmt.Errorf("tmux %s: gave up waiting after %s", strings.Join(command, " "), queryTimeout)
	}

	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("tmux %s: %w%s",
			strings.Join(command, " "), err, stderrDetail(err))
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
