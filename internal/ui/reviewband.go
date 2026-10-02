package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"

	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
)

// reviewBandFloor is how many rows the band leaves the rest of the frame: the blank row, the
// search prompt, and enough of the diff to be worth drawing.
const reviewBandFloor = 5

// reviewScopeOnTone is the half of the scope switch that is in force.
const reviewScopeOnTone = paint.TonePrimary

// bandView is what stands over the diff: what is being read, how big it is, what the reader
// has sent, and the pattern they are searching it for.
func (r reviewScreen) bandView(m *model, width int) string {
	kept := r.bandRows(m)

	out := strings.Join(kept, "") + "\n\n" + r.diff.filter.view(paint.Panel, m.glyphs) + diffMatchCount(&r.diff.Pane, r.diff.filter)

	if r.diff.View() != nil && r.diff.View().Len() > 0 {
		out += r.archivedView(m, width)
	}

	return out
}

// bandRows is the band's rows in the order they give way, least valuable last.
func (r reviewScreen) bandRows(m *model) []string {
	rows := []string{
		paint.Panel.Title("Review"+separated+r.agentName) + "\n",
		r.statusView(m),
		r.scopeView(m),
		r.diff.statView(),
		r.staleView(m),
		r.threads.deliveryCountView(),
	}

	// The title and the status line are what the screen is, so they are never dropped however
	// short the pane.
	const pinned = 2

	room := len(rows)
	if m.term.height > 0 {
		room = max(pinned, m.term.height-reviewBandFloor)
	}

	if room >= len(rows) {
		return rows
	}

	return rows[:room]
}

// archivedView says the agent is archived.
func (r reviewScreen) archivedView(m *model, width int) string {
	reason, archived := r.isReadOnly(m)
	if !archived {
		return ""
	}

	return reviewDeadStyle(paint.Panel).Render(reviewFill(" "+reason+" ", width)) + "\n"
}

// staleView says the worktree has moved since the diff on screen was read.
func (r reviewScreen) staleView(m *model) string {
	// The editor holds the stage rather than the diff, and the move it reports is usually the
	// reader's own writing, so there is nothing here worth a row.
	if !r.isWorktreeMoved(m) || r.diff.pinned || r.hold.atEditor {
		return "\n"
	}

	return paint.Panel.Style(reviewMovedTone).Render(r.staleSaid(m)) + "\n"
}

// staleSaid is why the diff is behind the worktree, in the terms the reader can act on.
func (r reviewScreen) staleSaid(m *model) string {
	if r.isAgentWriting(m) {
		return "the agent is writing" + separated + "the diff catches up when it stops"
	}

	if r.doesFollowLive(m) {
		return "the worktree has moved" + separated + "catching up"
	}

	return "the worktree has moved" + separated + keyLabel(m.keys.Review.Reload) + " to catch up"
}

// reviewFill pads a line out to the band's width, so a background colour reads as a bar rather
// than as a highlighted phrase.
func reviewFill(line string, pane int) string {
	width := max(20, pane-2)
	if run := lipgloss.Width(line); run < width {
		line += strings.Repeat(" ", width-run)
	}

	return line
}

// statView is how big the change is, with the counts coloured the way the lines themselves are.
func (d reviewDiff) statView() string {
	if d.View() == nil {
		return ""
	}

	stat := d.View().Stat()
	if stat.Files == 0 {
		return ""
	}

	out := dimStyle().Render(label.Counted(stat.Files, "file")+", ") +
		paint.Panel.Style(reviewAddedTone).Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		paint.Panel.Style(reviewRemovedTone).Render(fmt.Sprintf("-%d", stat.Removed))

	if read, total := d.View().CountViewedFiles(); read > 0 {
		out += dimStyle().Render(fmt.Sprintf("  %d/%d viewed", read, total))
	}

	return out + "\n"
}

// scopeView says what the diff on screen is measured between, in the reader's own terms rather
// than as rounds, and draws the other thing the base key could show beside it.
func (r reviewScreen) scopeView(m *model) string {
	if r.diff.View() == nil {
		return ""
	}

	from := "all changes"
	if r.diff.spec.label != "" {
		from = r.diff.spec.label
	}

	out := paint.Panel.Style(reviewScopeOnTone).Bold(true).Render(from)

	if r.doesFitBaseLegend(m) {
		out += dimStyle().Render(separated + r.stepSaid(m))
	}

	return out + "\n"
}

// stepSaid is the way through the chapters, naming only the direction that has somewhere to go.
func (r reviewScreen) stepSaid(m *model) string {
	back := keyLabel(m.keys.Review.Base)
	forward := keyLabel(m.keys.Review.BaseForward)

	switch {
	case r.diff.spec.origin != nil:
		return keyLabel(m.keys.Review.Jump) + " back"
	case !r.diff.spec.isChapter():
		return back + " for the latest chapter"
	case r.diff.spec.chapter == 0:
		return forward + " for a later chapter"
	default:
		return back + " back" + separated + forward + " forward"
	}
}

// bandRow draws one row of the band as a coloured state and a hint after it, cutting the hint
// rather than letting the row run past the pane.
func bandRow(m *model, tone paint.Tone, state, hint string) string {
	style := paint.Panel.Style(tone).Bold(true)

	width := lineWidthOf(m.paneWidth())

	room := width - lipgloss.Width(state) - len(separated)
	if room <= 0 {
		return style.Render(paint.Clip(state, width)) + "\n"
	}

	return style.Render(state) + dimStyle().Render(separated+paint.Clip(hint, room)) + "\n"
}

// reviewColumnStyle is the markdown a notice standing in for the patch is drawn in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
