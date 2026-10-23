package paneltest

import (
	"slices"

	"komrad/internal/panes"
	"komrad/internal/ui/pickfont"
	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// pickOrder is the order the picker hands its letters out in.
var pickOrder = []string{"d", "f", "j", "k", "l", "s", "a"}

// drawnLetter is a letter the picker drew, by the cell at its middle.
type drawnLetter struct {
	letter string
	x, y   int
}

// pick lands the keys on a sidebar list, record or block, or on a pane, through the pane picker.
func (p *Panel) pick(to string) {
	p.t.Helper()

	p.Press("ctrl+space", "j")

	letter := p.PickLetter(to)
	if letter == "" {
		p.t.Fatalf("the picker rendered no letter over %s", to)
	}

	p.Press(letter)
}

// PickLetter is the letter the pane picker drew over a sidebar list, record or block, or over a
// pane, while it holds the window.
func (p *Panel) PickLetter(to string) string {
	shown := slices.Clone(p.tmux.GetShownPanes(p.pane))
	if len(shown) == 0 {
		return ""
	}

	left, top := shown[0].Left, shown[0].Top
	for _, rect := range shown {
		left, top = min(left, rect.Left), min(top, rect.Top)
	}
	for i := range shown {
		shown[i].Left -= left
		shown[i].Top -= top
	}

	captures := p.tmux.captures()
	frozen := cells(ansi.Strip(p.panel.View().Content))
	drawn := findLetters(frozen, composeCaptures(shown, captures))

	if to != "list" && to != "record" && to != "block" {
		at := slices.IndexFunc(shown, func(rect panes.Rect) bool { return rect.Pane == to })
		if at < 0 {
			return ""
		}

		for _, letter := range drawn {
			if isInside(shown[at], letter.x, letter.y) {
				return letter.letter
			}
		}

		return ""
	}

	at := slices.IndexFunc(shown, func(rect panes.Rect) bool { return rect.Pane == p.pane })
	if at < 0 {
		return ""
	}
	sidebar := shown[at]

	var inside []drawnLetter
	for _, letter := range drawn {
		if isInside(sidebar, letter.x, letter.y) {
			inside = append(inside, letter)
		}
	}
	if len(inside) == 0 {
		return ""
	}
	slices.SortFunc(inside, func(a, b drawnLetter) int {
		return slices.Index(pickOrder, a.letter) - slices.Index(pickOrder, b.letter)
	})

	if to == "list" {
		return inside[0].letter
	}

	divider, hasDivider := findDivider(SplitLines(ansi.Strip(captures[p.pane])))
	for _, letter := range inside[1:] {
		isBlock := hasDivider && letter.x-sidebar.Left > divider
		if isBlock == (to == "block") {
			return letter.letter
		}
	}

	return ""
}

// findLetters is every letter the picker drew over the window, large or, where its target was too
// small, alone on cells it cleared.
func findLetters(frozen, before [][]string) []drawnLetter {
	var found []drawnLetter
	for _, letter := range pickfont.Letters() {
		glyph := SplitLines(pickfont.Render(letter, panes.Rect{Width: 1 << 10, Height: 1 << 10}))
		for y := range frozen {
			for x := range frozen[y] {
				if matchesGlyph(frozen, glyph, x, y) {
					found = append(found, drawnLetter{
						letter: letter,
						x:      x + len([]rune(glyph[0]))/2,
						y:      y + len(glyph)/2,
					})
				}
			}
		}
	}
	if len(found) > 0 {
		return found
	}

	for y, row := range frozen {
		for x, cell := range row {
			if !slices.Contains(pickOrder, cell) || cellAt(before, x, y) == cell {
				continue
			}
			if cellAt(frozen, x-1, y) != " " && x > 0 || cellAt(frozen, x+1, y) != " " && x+1 < len(row) {
				continue
			}

			found = append(found, drawnLetter{
				letter: cell,
				x:      x,
				y:      y,
			})
		}
	}

	return found
}

func matchesGlyph(frozen [][]string, glyph []string, x, y int) bool {
	if y+len(glyph) > len(frozen) {
		return false
	}

	for dy, row := range glyph {
		for dx, r := range []rune(row) {
			if cellAt(frozen, x+dx, y+dy) != string(r) {
				return false
			}
		}
	}

	return true
}

// composeCaptures lays each pane's captured screen where the pane sits in the window.
func composeCaptures(shown []panes.Rect, captures map[string]string) [][]string {
	var window [][]string
	for _, rect := range shown {
		for dy, row := range cells(ansi.Strip(captures[rect.Pane])) {
			for len(window) <= rect.Top+dy {
				window = append(window, nil)
			}
			line := window[rect.Top+dy]
			for len(line) < rect.Left+len(row) {
				line = append(line, " ")
			}
			copy(line[rect.Left:], row)
			window[rect.Top+dy] = line
		}
	}

	return window
}

// cells splits a frame into its cells, a wide character taking its own and an empty one after it.
func cells(frame string) [][]string {
	return xslice.Map(SplitLines(frame), func(line string) []string {
		var row []string
		for _, r := range line {
			row = append(row, string(r))
			for range ansi.StringWidth(string(r)) - 1 {
				row = append(row, "")
			}
		}

		return row
	})
}

func cellAt(grid [][]string, x, y int) string {
	if y < 0 || y >= len(grid) || x < 0 || x >= len(grid[y]) {
		return ""
	}

	return grid[y][x]
}

func isInside(rect panes.Rect, x, y int) bool {
	return x >= rect.Left && x < rect.Left+rect.Width && y >= rect.Top && y < rect.Top+rect.Height
}
