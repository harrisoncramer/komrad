package ui

import (
	"fmt"
	"strings"

	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
)

// reviewBandFloor is how many rows the band leaves the rest of the frame: the blank row, the
// search prompt, and enough of the diff to be worth drawing.
const reviewBandFloor = 5

// reviewScopeOnTone is the half of the scope switch that is in force.
const reviewScopeOnTone = paint.ToneAccent

// bandView is what stands over the diff: what is being read, how big it is, what the reader
// has sent, and the pattern they are searching it for.
func (r reviewScreen) bandView(s panelShared, width int) string {
	kept := r.bandRows(s)

	out := strings.Join(kept, "") + "\n\n" + r.diff.filter.view(paint.Panel) + r.diff.matchCount()

	if r.diff.view != nil && r.diff.view.Len() > 0 {
		out += r.deadView(s, width)
	}

	return out
}

// bandRows is the band's rows in the order they give way, least valuable last.
func (r reviewScreen) bandRows(s panelShared) []string {
	rows := []string{
		paint.Panel.Title("Review · "+r.agentName) + "\n",
		r.modeView(s),
		r.scopeView(s),
		r.diff.statView(),
		r.staleView(s),
		r.threads.deliveryCountView(),
	}

	// The title and the mode line are what the screen is, so they are never dropped however
	// short the pane.
	const pinned = 2

	room := len(rows)
	if s.height > 0 {
		room = max(pinned, s.height-reviewBandFloor)
	}

	if room >= len(rows) {
		return rows
	}

	return rows[:room]
}

// deadView says there is nobody on the other end.
func (r reviewScreen) deadView(s panelShared, width int) string {
	reason, dead := r.isReadOnly(s)
	if !dead {
		return ""
	}

	return reviewDeadStyle(paint.Panel).Render(reviewFill(" "+reason+" ", width)) + "\n"
}

// staleView says the worktree has moved since the diff on screen was read.
func (r reviewScreen) staleView(s panelShared) string {
	// The editor holds the stage rather than the diff, and the move it reports is usually the
	// reader's own writing, so there is nothing here worth a row.
	if !r.isWorktreeMoved(s) || r.diff.pinned || r.pass.atEditor {
		return "\n"
	}

	return paint.Panel.Style(reviewMovedTone).Render(r.staleSaid(s)) + "\n"
}

// staleSaid is why the diff is behind the worktree, in the terms the reader can act on.
func (r reviewScreen) staleSaid(s panelShared) string {
	if r.isAgentWriting(s) {
		return "the agent is writing · the diff catches up when it stops"
	}

	if r.doesFollowLive(s) {
		return "the worktree has moved · catching up"
	}

	if r.threads.holdsUnsent() {
		return "the worktree has moved · your comments are pinned to this round · " +
			keyLabel(s.keys.Review.Reload) + " to catch up"
	}

	return "the worktree has moved · " + keyLabel(s.keys.Review.Reload) + " to catch up"
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
	if d.view == nil {
		return ""
	}

	stat := d.view.Stat()
	if stat.Files == 0 {
		return ""
	}

	out := dimStyle.Render(label.Counted(stat.Files, "file")+", ") +
		paint.Panel.Style(reviewAddedTone).Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		paint.Panel.Style(reviewRemovedTone).Render(fmt.Sprintf("-%d", stat.Removed))

	if read, total := d.view.CountViewedFiles(); read > 0 {
		out += dimStyle.Render(fmt.Sprintf("  %d/%d viewed", read, total))
	}

	return out + "\n"
}

// scopeView says what the diff on screen is measured between, in the reader's own terms rather
// than as rounds, and draws the other thing the base key could show beside it.
func (r reviewScreen) scopeView(s panelShared) string {
	if r.diff.view == nil {
		return ""
	}

	from := "all changes"
	if r.diff.baseLabel != "" {
		from = r.diff.baseLabel
	}

	out := paint.Panel.Style(reviewScopeOnTone).Bold(true).Render(from)

	if r.doesFitBaseLegend(s) {
		out += dimStyle.Render(" · " + r.stepSaid(s))
	}

	return out + "\n"
}

// stepSaid is the way through the rounds, naming only the direction that has somewhere to go.
func (r reviewScreen) stepSaid(s panelShared) string {
	back := keyLabel(s.keys.Review.Base)
	forward := keyLabel(s.keys.Review.BaseForward)

	switch {
	case r.diff.chapter < 0:
		return back + " for the latest round"
	case r.diff.chapter == 0:
		return forward + " for a later round"
	default:
		return back + " back · " + forward + " forward"
	}
}

// matchCount is where in the search the cursor is standing, drawn under the pattern the panel's
// own filter is already showing.
func (d reviewDiff) matchCount() string {
	if d.view == nil || !d.filter.isStanding() {
		return ""
	}

	where := label.Counted(d.view.MatchCount(), "match")
	if at, ok := d.view.MatchNumber(d.cursor); ok {
		where = label.OfCount(at, d.view.MatchCount())
	}

	// A match inside a file folded away is on no row, so stepping through the matches never
	// reaches it, and it is counted apart rather than left to look like nothing was found.
	if hidden := d.view.HiddenMatches(); hidden > 0 {
		where += fmt.Sprintf(" · %d in viewed files", hidden)
	}

	return dimStyle.Render("  "+where) + "\n\n"
}

// bandRow draws one row of the band as a coloured state and a hint after it, cutting the hint
// rather than letting the row run past the pane.
func bandRow(s panelShared, tone paint.Tone, state, hint string) string {
	style := paint.Panel.Style(tone).Bold(true)

	width := lineWidthOf(s.pane)

	room := width - lipgloss.Width(state) - len(" · ")
	if room <= 0 {
		return style.Render(paint.Clip(state, width)) + "\n"
	}

	return style.Render(state) + dimStyle.Render(" · "+paint.Clip(hint, room)) + "\n"
}

// reviewColumnStyle is the markdown a notice standing in for the patch is drawn in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
