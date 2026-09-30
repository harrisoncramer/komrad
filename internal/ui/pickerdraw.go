package ui

import (
	"strings"

	"komrad/internal/panes"
	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
)

// tabStop is how far apart tmux sets a pane's tab stops.
const tabStop = 8

// pickerLabel is the label drawn over the sidebar while the picker holds the window.
const pickerLabel = " PANES "

// pickMargin is the blank cells cleared around a letter, across and down.
const (
	pickMarginX = 3
	pickMarginY = 1
)

// frozenWindow is every pane on show in the panel's window and what each was showing.
type frozenWindow struct {
	shown   []panes.Rect
	screens map[string]string
	sidebar string
	palette paint.Palette
}

// drawPicker lays each pane's screen where the pane sat, with a letter drawn over each
// target on a patch of blank cells.
func (w frozenWindow) drawPicker(targets []pickTarget) string {
	shown, palette := w.shown, w.palette
	if len(shown) == 0 {
		return ""
	}

	left, top, width, height := measureBounds(shown)
	letter := lipgloss.NewStyle().Foreground(palette.Color(paint.ToneHighlight)).Bold(true)

	layers := make([]*lipgloss.Layer, 0, len(shown)+2*len(targets)+1)
	for _, rect := range shown {
		layers = append(layers, lipgloss.NewLayer(fitScreen(w.screens[rect.Pane], rect)).X(rect.Left-left).Y(rect.Top-top))

		if rect.Pane == w.sidebar {
			layers = append(layers, w.drawLabelLayer(rect, left, top))
		}
	}

	for _, target := range targets {
		glyph := drawLetter(target.letter, target.rect)
		glyphWidth, glyphHeight := lipgloss.Width(glyph), strings.Count(glyph, "\n")+1
		clearWidth, clearHeight := glyphWidth+2*pickMarginX, glyphHeight+2*pickMarginY
		if clearWidth > target.rect.Width || clearHeight > target.rect.Height {
			clearWidth, clearHeight = glyphWidth, glyphHeight
		}

		x := target.rect.Left - left + (target.rect.Width-clearWidth)/2
		y := target.rect.Top - top + (target.rect.Height-clearHeight)/2
		blank := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", clearWidth)+"\n", clearHeight), "\n")

		layers = append(layers,
			lipgloss.NewLayer(blank).X(x).Y(y).Z(1),
			lipgloss.NewLayer(letter.Render(glyph)).X(x+(clearWidth-glyphWidth)/2).Y(y+(clearHeight-glyphHeight)/2).Z(2),
		)
	}

	return compose(width, height, layers)
}

func measureBounds(shown []panes.Rect) (left, top, width, height int) {
	left, top = shown[0].Left, shown[0].Top
	right, bottom := 0, 0

	for _, rect := range shown {
		left, top = min(left, rect.Left), min(top, rect.Top)
		right, bottom = max(right, rect.Left+rect.Width), max(bottom, rect.Top+rect.Height)
	}

	return left, top, right - left, bottom - top
}

func (w frozenWindow) drawLabelLayer(rect panes.Rect, left, top int) *lipgloss.Layer {
	label := w.palette.Visual().Render(pickerLabel)

	return lipgloss.NewLayer(label).
		X(rect.Left - left + rect.Width - lipgloss.Width(label) - gutter - 1).
		Y(rect.Top - top).
		Z(3)
}

func compose(width, height int, layers []*lipgloss.Layer) string {
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(layers...))

	return canvas.Render()
}

// fitScreen is a captured screen cut to its pane's rows, with its tabs spent as the spaces tmux
// moved the cursor over.
func fitScreen(screen string, rect panes.Rect) string {
	lines := strings.Split(strings.TrimSuffix(screen, "\n"), "\n")
	if len(lines) > rect.Height {
		lines = lines[:rect.Height]
	}

	for i, line := range lines {
		lines[i] = expandTabs(line)
	}

	return strings.Join(lines, "\n")
}

func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}

	var b strings.Builder

	col := 0
	for i, piece := range strings.Split(line, "\t") {
		if i > 0 {
			pad := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		}

		b.WriteString(piece)
		col += lipgloss.Width(piece)
	}

	return b.String()
}
