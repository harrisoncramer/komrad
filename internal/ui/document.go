package ui

import (
	"fmt"
	"strings"
)

// docScrollStep is how far a scroll key moves a document, kept short of a screenful so the
// reader keeps a few lines of what they were already on.
const docScrollStep = 10

// docWindow is where in a document the reader is: the rows it was rendered to, the first of them
// on screen, and whether it counts off what is past either end.
type docWindow struct {
	rows []string
	top  int
	// markers is whether the window counts the rows above and below it, which a document read
	// from the top wants and a transcript opened on a match does not, since the match is what
	// says where in it the reader is.
	markers bool
}

// clamp keeps the window inside the document, since the cursor can land on a shorter one while
// the offset stands and a window past the end would draw nothing.
func (d docWindow) clamp(top, rows int) int {
	if len(d.rows) <= rows {
		return 0
	}

	// The marker saying what is above takes a row of its own once the reader has scrolled, so a
	// document that counts them ends one row further down than the window is tall.
	end := len(d.rows) - rows
	if d.markers {
		end++
	}

	return max(min(top, end), 0)
}

// scroll moves the document under the list, which is the whole of what the pager these columns
// replaced was doing once it had opened where it was told to.
func (d *docWindow) scroll(delta, rows int) {
	d.top = d.clamp(d.top+delta, rows)
}

// view is the screenful of the document from the row the reader has scrolled to, padded out
// rather than left short so a document smaller than the window keeps its half of it instead of
// the list sliding under.
func (d docWindow) view(rows int) string {
	top := d.clamp(d.top, rows)

	out := make([]string, 0, rows)
	if d.markers && top > 0 {
		out = append(out, dimStyle.Render(fmt.Sprintf("  ↑ %d more", top)))
	}

	// The marker under the text takes a row as well, so what is left over is one row shorter
	// again whenever the document runs past the window.
	end := min(top+max(rows-len(out), 0), len(d.rows))
	if d.markers && end < len(d.rows) {
		end = max(end-1, top)
	}

	out = append(out, d.rows[top:end]...)
	if rest := len(d.rows) - end; d.markers && rest > 0 && len(out) < rows {
		out = append(out, dimStyle.Render(fmt.Sprintf("  ↓ %d more", rest)))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(out, "\n")
}

// docRows renders markdown into a column of its own, a row at a time, which is what a document
// drawn in the panel's own frame is scrolled and measured against.
func docRows(markdown string, width int) []string {
	return strings.Split(strings.TrimRight(render(markdown, width), "\n"), "\n")
}
