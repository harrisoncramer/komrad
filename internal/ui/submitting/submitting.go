// Package submitting draws the writes the panel has sent and is waiting on.
package submitting

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

const patience = 2 * time.Second

const separated = " " + glyph.Separator + " "

const detachHint = "esc to keep working"

// Write is one write in flight.
type Write struct {
	Doing string
	Since time.Time
}

// Params is what a write is drawn with.
type Params struct {
	Palette paint.Palette
	Frame   string
	Now     time.Time
	Width   int
}

func (w Write) elapsed(now time.Time) (time.Duration, bool) {
	if w.Since.IsZero() {
		return 0, false
	}

	elapsed := now.Sub(w.Since)

	return elapsed, elapsed >= patience
}

func (w Write) label() string {
	return w.Doing + "…"
}

func spinner(params Params, slow bool) string {
	tone := paint.ToneHighlight
	if slow {
		tone = paint.ToneWarning
	}

	return params.Palette.Style(tone).Render(params.Frame)
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return strconv.Itoa(int(elapsed.Seconds())) + "s"
	}

	return fmt.Sprintf("%dm%02ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
}

// Notice is the line drawn at the bottom of a form held for w.
func (w Write) Notice(params Params) string {
	elapsed, slow := w.elapsed(params.Now)
	head := spinner(params, slow) + " " + w.label()
	muted := params.Palette.Style(paint.ToneMuted)

	if !slow {
		return fit(head, params.Width)
	}

	counted := head + muted.Render(" "+formatElapsed(elapsed))
	hinted := counted + muted.Render(separated+detachHint)

	for _, line := range []string{hinted, counted} {
		if params.Width <= 0 || ansi.StringWidth(line) <= params.Width {
			return line
		}
	}

	return fit(head, params.Width)
}

// Badge is the newest of the writes in flight, drawn in the corner of the sidebar.
func Badge(writes []Write, params Params) string {
	if len(writes) == 0 {
		return ""
	}

	newest := slices.MaxFunc(writes, func(a, b Write) int { return a.Since.Compare(b.Since) })
	_, slow := newest.elapsed(params.Now)
	head := spinner(params, slow)

	more := ""
	if len(writes) > 1 {
		more = params.Palette.Style(paint.ToneMuted).Render(" +" + strconv.Itoa(len(writes)-1))
	}

	if params.Width <= 0 {
		return head + " " + newest.label() + more
	}

	room := params.Width - ansi.StringWidth(head+more) - 1
	if room < 2 {
		return fit(head+more, params.Width)
	}

	return head + " " + ansi.Truncate(newest.label(), room, "…") + more
}

func fit(line string, width int) string {
	if width <= 0 {
		return line
	}

	return ansi.Truncate(line, width, "…")
}
