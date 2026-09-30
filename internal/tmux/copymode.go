package tmux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"komrad/internal/panes"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// copyModeTable is the key table tmux reads keys from in copy-mode with vi keys.
const copyModeTable = "copy-mode-vi"

// standInName is the name of the window a stand-in waits in until it is swapped in.
const standInName = "komrad-copy"

// standInCopyOption is the pane option naming the directory a stand-in's copy is cut to its block from.
const standInCopyOption = "@komrad_copy"

// cellSeparator divides a row of the block's cells in the file a copy is cut from.
const cellSeparator = "\x1f"

// cutCopy is what y in a stand-in pipes the selection through.
const cutCopy = `sh #{` + standInCopyOption + `}/cut "#{socket_path}" "#{copy-command}" ` +
	`#{selection_start_x} #{selection_start_y} #{selection_end_x} #{selection_end_y} #{rectangle_toggle}`

// cutScript keeps the cells of a selection that fall inside the block and puts them on the clipboard.
const cutScript = `cat >/dev/null
socket=$1 copy=$2
out=$(awk -F "$(printf '\037')" -v top=%d -v left=%d -v sx="$3" -v sy="$4" -v ex="$5" -v ey="$6" -v rect="$7" '
BEGIN {
	if (sy > ey || (sy == ey && sx > ex)) { t = sx; sx = ex; ex = t; t = sy; sy = ey; ey = t }
	if (rect && sx > ex) { t = sx; sx = ex; ex = t }
}
{
	y = top + NR - 1
	if (y < sy || y > ey) next
	from = left; to = left + NF - 1
	if ((rect || y == sy) && sx > from) from = sx
	if ((rect || y == ey) && ex < to) to = ex
	s = ""
	for (c = from; c <= to; c++) s = s $(c - left + 1)
	sub(/ +$/, "", s)
	printf "%%s%%s", sep, s
	sep = "\n"
}' "$(dirname "$0")/cells")
printf '%%s' "$out" | %s -S "$socket" load-buffer -w -
if [ -n "$copy" ]; then printf '%%s' "$out" | sh -c "$copy"; fi
`

// clipboardCommands are the tools copy-mode can pipe a copy into, the first one installed winning.
var clipboardCommands = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
}

// TuneCopyMode sets copy-mode up as the panel's visual mode, with vi keys, badge over the top
// right of the pane, and a copy landing on the system clipboard.
func (c *Client) TuneCopyMode(badge string) {
	c.SetGlobal("mode-keys", "vi")
	c.SetGlobal("set-clipboard", "on")
	c.SetGlobal("copy-mode-position-format", "#[align=right]"+badge)

	if command := findClipboardCommand(); command != "" {
		c.SetGlobal("copy-command", command)
	}

	c.BindTableKey(copyModeTable, "v", "send-keys", "-X", "begin-selection")
	c.BindTableKey(copyModeTable, "C-v", "send-keys -X begin-selection ; send-keys -X rectangle-on")
	c.BindTableKey(copyModeTable, "y", "if-shell", "-F", "#{"+standInCopyOption+"}",
		"send-keys -X pipe-and-cancel '"+cutCopy+"'", "send-keys -X copy-pipe-and-cancel")
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

	cut, err := writeCut(text, params.Block, tmuxPath)
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
		restore = append(restore, "run-shell -b 'sleep 5; rm -rf "+cut+"'")
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

// writeCut writes the directory a copy in the stand-in is cut to block from, or nothing for a block
// with no cells.
func writeCut(text string, block panes.Rect, tmuxPath string) (string, error) {
	if block.Width <= 0 || block.Height <= 0 {
		return "", nil
	}

	dir, err := os.MkdirTemp("", "komrad-copy-*")
	if err != nil {
		return "", fmt.Errorf("failed to hold the block to copy from: %w", err)
	}

	lines := strings.Split(text, "\n")
	rows := make([]string, 0, block.Height)
	for y := block.Top; y < block.Top+block.Height; y++ {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}

		cells := splitCells(ansi.Strip(line))
		row := make([]string, block.Width)
		for i := range row {
			row[i] = " "
			if at := block.Left + i; at < len(cells) {
				row[i] = cells[at]
			}
		}

		rows = append(rows, strings.Join(row, cellSeparator))
	}

	script := fmt.Sprintf(cutScript, block.Top, block.Left, "'"+strings.ReplaceAll(tmuxPath, "'", `'\''`)+"'")
	writeErr := errors.Join(
		os.WriteFile(filepath.Join(dir, "cells"), []byte(strings.Join(rows, "\n")+"\n"), 0o600),
		os.WriteFile(filepath.Join(dir, "cut"), []byte(script), 0o600),
	)
	if writeErr != nil {
		return "", errors.Join(fmt.Errorf("failed to hold the block to copy from: %w", writeErr), os.RemoveAll(dir))
	}

	return dir, nil
}

func removeCut(dir string) error {
	if dir == "" {
		return nil
	}

	return os.RemoveAll(dir)
}

// splitCells is a row as the cells it fills, a wide character's second cell left empty.
func splitCells(plain string) []string {
	var cells []string

	graphemes := uniseg.NewGraphemes(plain)
	for graphemes.Next() {
		width := graphemes.Width()
		if width == 0 {
			if len(cells) > 0 {
				cells[len(cells)-1] += graphemes.Str()
			}

			continue
		}

		cells = append(cells, graphemes.Str())
		for range width - 1 {
			cells = append(cells, "")
		}
	}

	return cells
}
