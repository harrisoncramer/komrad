package tmux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"komrad/internal/panes"
)

// copyModeTable is the key table tmux reads keys from in copy-mode with vi keys.
const copyModeTable = "copy-mode-vi"

// standInName is the name of the window a stand-in waits in until it is swapped in.
const standInName = "komrad-copy"

// standInCopyOption is the pane option naming the file a stand-in's copy is cut to its block from.
const standInCopyOption = "@komrad_copy"

// copierOption is the global option naming the binary a stand-in's copy is cut by.
const copierOption = "@komrad_copier"

// cutCopy is what a copy in a stand-in pipes the selection through.
const cutCopy = `#{q:` + copierOption + `} copy #{q:` + standInCopyOption + `} ` +
	`#{selection_start_x} #{selection_start_y} #{selection_end_x} #{selection_end_y} #{rectangle_toggle} ` +
	`--socket=#{q:socket_path} --copy-command=#{q:copy-command}`

// copySelection copies the selection as tmux does, or only the cells inside the block in a stand-in.
const copySelection = `if-shell -F "#{` + standInCopyOption + `}" "send-keys -X pipe-and-cancel '` + cutCopy + `'" "send-keys -X copy-pipe-and-cancel"`

// clipboardCommands are the tools copy-mode can pipe a copy into, the first one installed winning.
var clipboardCommands = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
}

// TuneCopyMode sets copy-mode up as the panel's visual mode, with vi keys, badge over the top
// right of the pane, and a copy landing on the system clipboard, cut to its block by copier in a
// stand-in.
func (c *Client) TuneCopyMode(badge, copier string) {
	c.SetGlobal("mode-keys", "vi")
	c.SetGlobal("set-clipboard", "on")
	c.SetGlobal("copy-mode-position-format", "#[align=right]"+badge)
	c.SetGlobal(copierOption, copier)

	if command := findClipboardCommand(); command != "" {
		c.SetGlobal("copy-command", command)
	}

	c.BindTableKey(copyModeTable, "v", "send-keys", "-X", "begin-selection")
	c.BindTableKey(copyModeTable, "C-v", "send-keys -X begin-selection ; send-keys -X rectangle-on")
	for _, key := range []string{"y", "Enter", "C-j", "MouseDragEnd1Pane"} {
		c.BindTableKey(copyModeTable, key, copySelection)
	}
	c.BindTableKey(copyModeTable, "D", "send-keys -X begin-selection ; send-keys -X end-of-line ; "+copySelection)
	c.BindTableKey(copyModeTable, "DoubleClick1Pane", "select-pane ; send-keys -X select-word ; run-shell -d 0.3 ; "+copySelection)
	c.BindTableKey(copyModeTable, "TripleClick1Pane", "select-pane ; send-keys -X select-line ; run-shell -d 0.3 ; "+copySelection)
	c.BindTableKey(copyModeTable, "Escape", "if-shell", "-F", "#{selection_present}",
		"send-keys -X clear-selection", "send-keys -X cancel")
}

func findClipboardCommand() string {
	for _, command := range clipboardCommands {
		if _, err := exec.LookPath(command[0]); err == nil {
			return strings.Join(command, " ")
		}
	}

	return ""
}

// OpenCopyStandIn swaps a pane printing text into pane's place and puts it in copy-mode with the
// cursor at row and col, and once copy-mode ends swaps pane back, sends it keys and kills the
// stand-in.
func (c *Client) OpenCopyStandIn(params panes.CopyStandIn) error {
	pane, text, row, col, keys := params.Pane, params.Text, params.Row, params.Col, params.Keys
	if pane == "" {
		return errors.New("refusing to stand in for an empty pane")
	}

	shape, err := c.query("display-message", "-p", "-t", pane, "#{pane_width} #{pane_height} #{session_name}")
	if err != nil {
		return fmt.Errorf("failed to read pane %q: %w", pane, err)
	}

	width, height, session, err := parseShape(shape)
	if err != nil {
		return fmt.Errorf("failed to read pane %q: %w", pane, err)
	}

	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("failed to find tmux: %w", err)
	}

	file, err := writeStandInText(text)
	if err != nil {
		return err
	}

	cut, err := writeCut(text, params.Block)
	if err != nil {
		return errors.Join(err, os.Remove(file))
	}

	standIn, err := c.query("new-window", "-d", "-t", session+":", "-n", standInName, "-P", "-F", "#{pane_id}", "sleep 2147483647")
	if err != nil {
		return errors.Join(fmt.Errorf("failed to open a stand-in for pane %q: %w", pane, err), os.Remove(file), removeCut(cut))
	}

	c.ResizeWindow(standIn, width, height)

	if cut != "" {
		if _, err := c.query("set-option", "-p", "-t", standIn, standInCopyOption, cut); err != nil {
			c.KillPane(standIn)

			return errors.Join(fmt.Errorf("failed to mark the stand-in for pane %q: %w", pane, err), os.Remove(file), removeCut(cut))
		}
	}

	script := fmt.Sprintf(`cat "$0"; rm -f "$0"; printf '\033[%d;%dH'; "$1" copy-mode -t "$TMUX_PANE"; exec sleep 2147483647`, max(row, 1), max(col, 1))
	if err := c.RespawnPane(standIn, "", "sh", "-c", script, file, tmuxPath); err != nil {
		c.KillPane(standIn)

		return errors.Join(err, os.Remove(file), removeCut(cut))
	}

	restore := []string{"swap-pane -Z -s " + standIn + " -t " + pane, "select-pane -t " + pane}
	if len(keys) > 0 {
		restore = append(restore, "send-keys -t "+pane+" "+strings.Join(keys, " "))
	}
	if cut != "" {
		restore = append(restore, "run-shell -b -t "+standIn+" 'sleep 5; rm -f #{q:"+standInCopyOption+"}'")
	}
	restore = append(restore, "kill-pane -t "+standIn)

	hook := `if-shell -F -t ` + standIn + ` "#{pane_in_mode}" "" "` + strings.Join(restore, " ; ") + `"`
	if _, err := c.query("set-hook", "-p", "-t", standIn, "pane-mode-changed", hook); err != nil {
		c.KillPane(standIn)

		return errors.Join(fmt.Errorf("failed to watch the stand-in for pane %q: %w", pane, err), removeCut(cut))
	}

	if _, err := c.query("swap-pane", "-Z", "-s", standIn, "-t", pane); err != nil {
		c.KillPane(standIn)

		return errors.Join(fmt.Errorf("failed to swap in the stand-in for pane %q: %w", pane, err), removeCut(cut))
	}

	c.SelectPane(standIn)

	return nil
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

func writeStandInText(text string) (string, error) {
	file, err := os.CreateTemp("", "komrad-copy-*")
	if err != nil {
		return "", fmt.Errorf("failed to hold the text to copy: %w", err)
	}

	_, writeErr := file.WriteString(text)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(fmt.Errorf("failed to hold the text to copy: %w", err), os.Remove(file.Name()))
	}

	return file.Name(), nil
}
