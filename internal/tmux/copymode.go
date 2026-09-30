package tmux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"komrad/internal/panes"
	"komrad/lib/xslice"
)

// copyModeTable is the key table tmux reads keys from in copy-mode with vi keys.
const copyModeTable = "copy-mode-vi"

// standInName is the name of the window a stand-in waits in until it is swapped in.
const standInName = "komrad-copy"

// copySelection copies the selection into tmux's buffers, the clipboard and the copy-command.
const copySelection = "send-keys -X copy-pipe-and-cancel"

// clipboardCommands are the tools copy-mode can pipe a copy into, the first one installed winning.
var clipboardCommands = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
}

// TuneCopyMode sets copy-mode up as the panel's visual mode, with vi keys, no badge over the pane,
// a selection drawn in selection, and a copy landing on the system clipboard.
func (c *Client) TuneCopyMode(selection string) {
	for _, command := range tuneCopyModeCommands(selection) {
		c.run(command...)
	}
}

// OpenCopyStandIn opens a hidden window running the stand-in that visual mode draws params in,
// which swaps itself into the pane's place and swaps the pane back once copy-mode ends.
func (c *Client) OpenCopyStandIn(params panes.CopyStandIn) error {
	if params.Pane == "" {
		return errors.New("refusing to stand in for an empty pane")
	}

	shape, err := c.query("display-message", "-p", "-t", params.Pane, "#{pane_width} #{pane_height} #{session_name}")
	if err != nil {
		return fmt.Errorf("failed to read pane %q: %w", params.Pane, err)
	}

	width, height, session, err := parseShape(shape)
	if err != nil {
		return fmt.Errorf("failed to read pane %q: %w", params.Pane, err)
	}

	place, err := c.query("display-message", "-p", "-t", params.Pane, "#{window_id} #{window_zoomed_flag} #{socket_path}")
	if err != nil {
		return fmt.Errorf("failed to read the window of pane %q: %w", params.Pane, err)
	}

	fields := strings.SplitN(place, " ", 3)
	if len(fields) != 3 {
		return fmt.Errorf("unexpected window of pane %q: %q", params.Pane, place)
	}

	selection, err := c.query("display-message", "-p", "-t", params.Pane, "#{mode-style}")
	if err != nil {
		return fmt.Errorf("failed to read the selection style of pane %q: %w", params.Pane, err)
	}

	standIn, err := c.query("new-window", "-d", "-t", session+":", "-n", standInName, "-P", "-F", "#{pane_id}", "sleep 2147483647")
	if err != nil {
		return fmt.Errorf("failed to open a stand-in for pane %q: %w", params.Pane, err)
	}

	c.ResizeWindow(standIn, width, height)
	if drawn, err := c.query("display-message", "-p", "-t", standIn, "#{pane_width} #{pane_height} #{session_name}"); err == nil {
		if drawnWidth, drawnHeight, _, err := parseShape(drawn); err == nil && (drawnWidth != width || drawnHeight != height) {
			c.ResizeWindow(standIn, 2*width-drawnWidth, 2*height-drawnHeight)
		}
	}

	file, err := writeStandInSpec(standInSpec{
		Socket:      fields[2],
		StandIn:     standIn,
		Sidebar:     params.Pane,
		Window:      fields[0],
		Zoomed:      fields[1] == "1",
		Width:       width,
		Height:      height,
		BorderStyle: params.BorderStyle,
		Selection:   selection,
		Rows:        params.Rows,
		Divider:     params.Divider,
		Rule:        params.Rule,
		RuleLeft:    params.RuleLeft,
		FocusX:      params.Focus.Left + params.Focus.Width/2,
		FocusY:      params.Focus.Top + params.Focus.Height/2,
		Row:         params.Row,
		Col:         params.Col,
		Keys:        params.Keys,
	})
	if err != nil {
		c.KillPane(standIn)

		return err
	}

	if err := c.RespawnPane(standIn, "", params.Binary, "visual", file); err != nil {
		c.KillPane(standIn)

		return errors.Join(err, os.Remove(file))
	}

	return nil
}

func tuneCopyModeCommands(selection string) [][]string {
	commands := [][]string{
		{"set-option", "-g", "mode-style", selection},
		{"set-option", "-g", "mode-keys", "vi"},
		{"set-option", "-g", "set-clipboard", "on"},
		{"set-option", "-gq", "copy-mode-position-format", ""},
	}

	if command := findClipboardCommand(); command != "" {
		commands = append(commands, []string{"set-option", "-g", "copy-command", command})
	}

	commands = append(commands,
		[]string{"bind-key", "-T", copyModeTable, "v", "send-keys", "-X", "begin-selection"},
		[]string{"bind-key", "-T", copyModeTable, "C-v", "send-keys -X begin-selection ; send-keys -X rectangle-on"},
	)
	commands = append(commands, xslice.Map([]string{"y", "Enter", "C-j", "MouseDragEnd1Pane"}, func(key string) []string {
		return []string{"bind-key", "-T", copyModeTable, key, copySelection}
	})...)

	return append(commands,
		[]string{"bind-key", "-T", copyModeTable, "D", "send-keys -X begin-selection ; send-keys -X end-of-line ; " + copySelection},
		[]string{"bind-key", "-T", copyModeTable, "DoubleClick1Pane", "select-pane ; send-keys -X select-word ; run-shell -d 0.3 ; " + copySelection},
		[]string{"bind-key", "-T", copyModeTable, "TripleClick1Pane", "select-pane ; send-keys -X select-line ; run-shell -d 0.3 ; " + copySelection},
		[]string{"bind-key", "-T", copyModeTable, "Escape", "if-shell", "-F", "#{selection_present}", "send-keys -X clear-selection", "send-keys -X cancel"},
	)
}

func findClipboardCommand() string {
	for _, command := range clipboardCommands {
		if _, err := exec.LookPath(command[0]); err == nil {
			return strings.Join(command, " ")
		}
	}

	return ""
}

func parseShape(shape string) (width, height int, session string, err error) {
	fields := strings.SplitN(shape, " ", 3)
	if len(fields) != 3 {
		return 0, 0, "", fmt.Errorf("unexpected pane shape %q", shape)
	}

	width, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to read pane width %q: %w", fields[0], err)
	}

	height, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to read pane height %q: %w", fields[1], err)
	}

	return width, height, fields[2], nil
}

func writeStandInSpec(spec standInSpec) (string, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("failed to hold the stand-in: %w", err)
	}

	file, err := os.CreateTemp("", "komrad-visual-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to hold the stand-in: %w", err)
	}

	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(fmt.Errorf("failed to hold the stand-in: %w", err), os.Remove(file.Name()))
	}

	return file.Name(), nil
}
