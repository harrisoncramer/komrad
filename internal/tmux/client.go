// Package tmux is komrad's seam in front of tmux, wrapping the one server it runs so the rest of
// the code names sessions, windows and panes rather than shell arguments.
//
// Every agent lives in a pane on that server, and the panel is a client attached to its manager
// session, so almost everything komrad does to an agent it does through here: creating the
// window, capturing the frame the reconciler reads its state out of, typing into it, and
// killing it. The socket is a field rather than a global so a test can point a client at a
// throwaway server and drive it for real.
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

	"komrad/internal/logging"
)

const paneOwnerOption = "@komrad_agent_id"

const hostSocketOption = "@komrad_host_socket"

const hostPathOption = "@komrad_host_path"

const InfoPaneOption = "@komrad_info_pane"

const panelPaneOption = "@komrad_panel_pane"

const panelBuildOption = "@komrad_panel_build"

const queryTimeout = 5 * time.Second

const killDelay = 100 * time.Millisecond

type Client struct {
	server []string
}

const DefaultSocket = "default"

// NewClient returns a Client bound to a tmux server by socket name.
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

// GetWindowWidth is how many columns the window holding a pane has, zero when it is gone.
func (c *Client) GetWindowWidth(pane string) int {
	if pane == "" {
		return 0
	}

	out, err := c.query("display-message", "-p", "-t", pane, "#{window_width}")
	if err != nil {
		return 0
	}

	width, err := strconv.Atoi(out)
	if err != nil {
		return 0
	}

	return width
}

// GetPaneHeight is how many rows a pane has.
func (c *Client) GetPaneHeight(pane string) int {
	if pane == "" {
		return 0
	}

	out, err := c.query("display-message", "-p", "-t", pane, "#{pane_height}")
	if err != nil {
		return 0
	}

	height, err := strconv.Atoi(out)
	if err != nil {
		return 0
	}

	return height
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

// IsPaneInMode reports whether a pane is in one of tmux's own modes, where keys reach the mode
// instead of the program running there.
func (c *Client) IsPaneInMode(pane string) bool {
	if pane == "" {
		return false
	}

	return c.value("display-message", "-p", "-t", pane, "#{pane_in_mode}") == "1"
}

type PaneStatus struct {
	Owner   string
	Command string
}

var shellCommands = map[string]bool{
	"sh": true, "-sh": true,
	"bash": true, "-bash": true,
	"dash": true, "-dash": true,
	"ash": true, "-ash": true,
	"zsh": true, "-zsh": true,
	"ksh": true, "-ksh": true,
	"csh": true, "-csh": true,
	"tcsh": true, "-tcsh": true,
	"fish": true, "-fish": true,
	"nu": true, "-nu": true,
}

// IsRunningShell reports whether the pane's foreground program is a plain shell.
func (p PaneStatus) IsRunningShell() bool {
	return shellCommands[p.Command]
}

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

// GetClientPath returns the working directory of the active pane of whichever client this server
// saw most recently, which only a command running on that server can ask.
func (c *Client) GetClientPath() string {
	return c.value("display-message", "-p", "#{pane_current_path}")
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

// SplitRight splits a pane right of the anchor, rooted at dir and sized in cells or as a
// percentage like "40%", leaving focus where it was.
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

// SelectPane moves focus to a pane.
func (c *Client) SelectPane(pane string) {
	c.run("select-pane", "-t", pane)
}

// SelectWindow makes the window holding a pane the session's current one.
func (c *Client) SelectWindow(pane string) {
	if pane == "" {
		return
	}

	c.run("select-window", "-t", pane)
}

// ResizePane sets a pane's width, as a count of columns or a percentage of the window.
func (c *Client) ResizePane(pane, width string) {
	c.run("resize-pane", "-t", pane, "-x", width)
}

// GetPaneFocus reports whether a pane is the one its window is sending keys to and whether it is
// filling that window, asked together because one query answers for both.
func (c *Client) GetPaneFocus(pane string) (focused, zoomed bool) {
	if pane == "" {
		return false, false
	}

	out, err := c.query("display-message", "-p", "-t", pane, "#{pane_active},#{window_zoomed_flag}")
	if err != nil {
		return false, false
	}

	active, window, _ := strings.Cut(out, ",")

	return active == "1", active == "1" && window == "1"
}

// GetActivePane is the pane the window holding pane is sending keys to, empty where tmux cannot
// say.
func (c *Client) GetActivePane(pane string) string {
	if pane == "" {
		return ""
	}

	out, err := c.query("list-panes", "-t", pane, "-F", "#{pane_active} #{pane_id}")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(out, "\n") {
		if active, ok := strings.CutPrefix(line, "1 "); ok {
			return active
		}
	}

	return ""
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

// ResizeWindow sets the size of the window a pane sits in, so a window nobody is watching is
// kept at the size of the slot its pane will be shown in.
func (c *Client) ResizeWindow(pane string, width, height int) {
	if pane == "" || width <= 0 || height <= 0 {
		return
	}

	c.run("resize-window", "-t", pane, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
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

// RespawnPane replaces whatever is running in a pane with a command, rooted at dir.
func (c *Client) RespawnPane(pane, dir string, argv ...string) error {
	if pane == "" {
		return errors.New("refusing to respawn an empty pane")
	}
	if len(argv) == 0 {
		return errors.New("refusing to respawn a pane with no command")
	}

	args := []string{"respawn-pane", "-k", "-t", pane}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	args = append(args, "--")
	args = append(args, argv...)

	if _, err := c.query(args...); err != nil {
		return fmt.Errorf("failed to respawn pane %q: %w", pane, err)
	}

	return nil
}

// KillPane tears down a single pane, treating one already gone as the outcome asked for.
func (c *Client) KillPane(pane string) {
	if pane == "" {
		return
	}

	if _, err := c.query("kill-pane", "-t", pane); err != nil && !strings.Contains(err.Error(), "can't find pane") {
		//nolint:sloglint // as in MovePanes, there is no context here to carry a logger on
		slog.Default().Warn("a tmux command failed", logging.Attrs(logging.Err(err))...)
	}
}

// KillWindow tears down the whole window a pane sits in, treating one already gone as the outcome
// asked for.
func (c *Client) KillWindow(pane string) {
	if pane == "" {
		return
	}

	if _, err := c.query("kill-window", "-t", pane); err != nil && !strings.Contains(err.Error(), "can't find") {
		//nolint:sloglint // as in MovePanes, there is no context here to carry a logger on
		slog.Default().Warn("a tmux command failed", logging.Attrs(logging.Err(err))...)
	}
}

// BindRootKey binds a key outside the prefix table, taking that key away from whatever is
// running in the panes.
func (c *Client) BindRootKey(key string, command ...string) {
	c.run(append([]string{"bind-key", "-n", key}, command...)...)
}

// UnbindRootKey gives a key bound outside the prefix table back to whatever is running in the panes.
func (c *Client) UnbindRootKey(key string) {
	c.run("unbind-key", "-n", key)
}

// BlankBorders draws every pane's border as blank cells, with a blank status line over each pane.
func (c *Client) BlankBorders() {
	c.SetGlobal("pane-border-lines", "spaces")
	c.SetGlobal("pane-border-status", "top")
	c.SetGlobal("pane-border-format", "")
	c.SetGlobal("fill-character", " ")
}

// GutterCells is the blank column right of the panel's window and the blank row under it.
const GutterCells = 1

// InsetPanelWindow keeps the panel's window a column narrower and a row shorter than each client
// of the session shows it at, so the blank tmux fills the rest with runs past every pane.
func (c *Client) InsetPanelWindow(session string) {
	resize := fmt.Sprintf(
		"run-shell -C \"resize-window -t #{%s} -x #{e|-:#{client_width},%d} -y #{e|-:#{client_height},%d}\"",
		panelPaneOption, GutterCells, GutterCells,
	)

	for _, hook := range []string{"client-attached", "client-resized", "client-session-changed"} {
		c.run("set-hook", "-t", session, hook, resize)
	}
}

// SetGlobal sets a global tmux option.
func (c *Client) SetGlobal(name, value string) {
	c.run("set-option", "-g", name, value)
}

// GetSocketPath returns the path of the socket this client's server listens on.
func (c *Client) GetSocketPath() (string, error) {
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

// SetInfoPane records which pane the panel opened its info region in, and clears it when given
// nothing.
func (c *Client) SetInfoPane(pane string) {
	c.SetGlobal(InfoPaneOption, pane)
}

// GetInfoPane reads that pane back, from the same server it was written on.
func (c *Client) GetInfoPane() string {
	return c.ShowGlobal(InfoPaneOption)
}

// RecordPanelPane stores the pane the panel itself draws in, so a later open can tell a panel
// that is still up from a session holding only agent windows.
func (c *Client) RecordPanelPane(pane string) {
	if pane == "" {
		return
	}

	c.SetGlobal(panelPaneOption, pane)
}

// GetPanelPane reads that pane back, empty when no panel has recorded one.
func (c *Client) GetPanelPane() string {
	return c.ShowGlobal(panelPaneOption)
}

// RecordPanelBuild stores the build the panel drawing in that pane was made from.
func (c *Client) RecordPanelBuild(build string) {
	if build == "" {
		return
	}

	c.SetGlobal(panelBuildOption, build)
}

// GetPanelBuild reads that build back, empty when the panel never recorded one.
func (c *Client) GetPanelBuild() string {
	return c.ShowGlobal(panelBuildOption)
}

// RecordHostSocket stores the socket path of the user's server, so the panel and everything
// reading it agree on one answer.
func (c *Client) RecordHostSocket(path string) {
	if path == "" {
		return
	}

	c.SetGlobal(hostSocketOption, path)
}

// GetHostSocket is the socket path of the user's server as the last open recorded it.
func (c *Client) GetHostSocket() string {
	return c.ShowGlobal(hostSocketOption)
}

// RecordHostPath stores where the client that opened the panel was sitting, an empty value being
// written just as deliberately.
func (c *Client) RecordHostPath(path string) {
	c.SetGlobal(hostPathOption, path)
}

// GetHostPath returns the working directory of the pane the panel was opened over.
func (c *Client) GetHostPath() string {
	return c.ShowGlobal(hostPathOption)
}

// EnableFocusEvents has tmux tell a pane when it gains or loses the keys, so the panel hears an
// editor pane quitting rather than waiting to notice the pane gone.
func (c *Client) EnableFocusEvents() {
	c.run("set-option", "-s", "focus-events", "on")
}

// SetSessionOption sets a session-scoped tmux option.
func (c *Client) SetSessionOption(target, name, value string) {
	c.run("set-option", "-t", target, name, value)
}

// GetDefaultShell returns the shell tmux starts a pane's process with.
func (c *Client) GetDefaultShell() string {
	return c.ShowGlobal("default-shell")
}

// DetachSession detaches every client from the named session, leaving its pane processes
// running.
func (c *Client) DetachSession(name string) {
	c.run("detach-client", "-s", name)
}

// CapturePane returns the visible content of a pane with colors preserved.
func (c *Client) CapturePane(pane string) string {
	return c.value("capture-pane", "-p", "-e", "-t", pane)
}

func (c *Client) query(args ...string) (string, error) {
	command := slices.Concat(c.server, args)

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", command...)
	cmd.WaitDelay = killDelay

	out, err := cmd.Output()

	if err != nil && ctx.Err() != nil {
		return "", fmt.Errorf("tmux %s: gave up waiting after %s", strings.Join(command, " "), queryTimeout)
	}

	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("tmux %s: %w%s",
			strings.Join(command, " "), err, stderrDetail(err))
	}

	return strings.TrimSpace(string(out)), nil
}

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

func (c *Client) run(args ...string) {
	if _, err := c.query(args...); err != nil {
		//nolint:sloglint // as in MovePanes, there is no context here to carry a logger on
		slog.Default().Warn("a tmux command failed", logging.Attrs(logging.Err(err))...)
	}
}

func (c *Client) value(args ...string) string {
	out, err := c.query(args...)
	if err != nil {
		//nolint:sloglint // as in MovePanes, there is no context here to carry a logger on
		slog.Default().Debug("a tmux query failed", logging.Attrs(logging.Err(err))...)
	}

	return out
}

func (c *Client) paneID(target string) (string, error) {
	return c.query("display-message", "-p", "-t", target, "#{pane_id}")
}

// IsPaneRunningCLI reports whether a pane's foreground program is something other than a plain
// shell.
func (c *Client) IsPaneRunningCLI(pane string) bool {
	if pane == "" {
		return false
	}

	command, err := c.query("display-message", "-p", "-t", pane, "#{pane_current_command}")
	if err != nil || command == "" {
		return false
	}

	return !PaneStatus{Command: command}.IsRunningShell()
}
