package tmux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"komrad/internal/panes"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Selection is where a selection in copy-mode runs, both ends inclusive and the start after the
// end when it was made backwards.
type Selection struct {
	StartX    int
	StartY    int
	EndX      int
	EndY      int
	Rectangle bool
}

// standInCut is what a stand-in's file holds for a copy to be cut from: the rows it printed and
// the block a copy keeps to.
type standInCut struct {
	Rows   []string `json:"rows"`
	Top    int      `json:"top"`
	Left   int      `json:"left"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
}

// CopyFromStandIn puts the part of sel inside the block held in file in tmux's buffers and on the
// clipboard, and pipes it into copyCommand when one is set.
func (c *Client) CopyFromStandIn(ctx context.Context, file string, sel Selection, copyCommand string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read the block to copy from: %w", err)
	}

	var cut standInCut
	if err := json.Unmarshal(data, &cut); err != nil {
		return fmt.Errorf("failed to read the block to copy from: %w", err)
	}

	text := cutSelection(cut, sel)
	if text == "" {
		return nil
	}

	if _, err := c.query("set-buffer", "-w", "--", text); err != nil {
		return fmt.Errorf("failed to hold the copy: %w", err)
	}

	if copyCommand == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", copyCommand)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run the copy-command %q: %w: %s", copyCommand, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// cutSelection is the text of the cells sel covers inside the block, one line per row.
func cutSelection(cut standInCut, sel Selection) string {
	sx, sy, ex, ey := sel.StartX, sel.StartY, sel.EndX, sel.EndY
	if sy > ey || (sy == ey && sx > ex) {
		sx, sy, ex, ey = ex, ey, sx, sy
	}
	if sel.Rectangle && sx > ex {
		sx, ex = ex, sx
	}

	var lines []string
	for y := max(sy, cut.Top); y <= min(ey, cut.Top+cut.Height-1); y++ {
		from, to := cut.Left, cut.Left+cut.Width-1
		if sel.Rectangle || y == sy {
			from = max(from, sx)
		}
		if sel.Rectangle || y == ey {
			to = min(to, ex)
		}

		var cells []string
		if y < len(cut.Rows) {
			cells = splitCells(ansi.Strip(cut.Rows[y]))
		}

		var line strings.Builder
		for x := from; x <= to && x < len(cells); x++ {
			line.WriteString(cells[x])
		}

		lines = append(lines, strings.TrimRight(line.String(), " "))
	}

	return strings.Join(lines, "\n")
}

// writeCut writes the file a copy in the stand-in is cut to block from, or nothing for a block
// with no cells.
func writeCut(text string, block panes.Rect) (string, error) {
	if block.Width <= 0 || block.Height <= 0 {
		return "", nil
	}

	data, err := json.Marshal(standInCut{
		Rows:   strings.Split(text, "\n"),
		Top:    block.Top,
		Left:   block.Left,
		Width:  block.Width,
		Height: block.Height,
	})
	if err != nil {
		return "", fmt.Errorf("failed to hold the block to copy from: %w", err)
	}

	file, err := os.CreateTemp("", "komrad-copy-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to hold the block to copy from: %w", err)
	}

	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(fmt.Errorf("failed to hold the block to copy from: %w", err), os.Remove(file.Name()))
	}

	return file.Name(), nil
}

func removeCut(file string) error {
	if file == "" {
		return nil
	}

	return os.Remove(file)
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
