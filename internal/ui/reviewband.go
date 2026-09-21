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

// reviewBandView is what stands over the diff: what is being read, how big it is, what the
// reader has sent, and the pattern they are searching it for.
func (m model) reviewBandView(width int) string {
	kept := m.reviewBandRows()

	out := strings.Join(kept, "") + "\n\n" + m.review.diff.filter.view(paint.Panel) + m.review.diff.matchCount()

	if m.review.diff.view != nil && m.review.diff.view.Len() > 0 {
		out += m.reviewDeadView(width)
	}

	return out
}

// reviewBandRows is the band's rows in the order they give way, least valuable last.
func (m model) reviewBandRows() []string {
	rows := []string{
		paint.Panel.Title("Review · "+m.review.agentName) + "\n",
		m.reviewModeView(),
		m.reviewScopeView(),
		m.review.diff.statView(),
		m.reviewStaleView(),
		m.review.threads.deliveryCountView(),
	}

	// The title and the mode line are what the screen is, so they are never dropped however
	// short the pane.
	const pinned = 2

	room := len(rows)
	if m.height > 0 {
		room = max(pinned, m.height-reviewBandFloor)
	}

	if room >= len(rows) {
		return rows
	}

	return rows[:room]
}

// reviewDeadView says there is nobody on the other end.
func (m model) reviewDeadView(width int) string {
	reason, dead := m.isReviewReadOnly()
	if !dead {
		return ""
	}

	return reviewDeadStyle(paint.Panel).Render(reviewFill(" "+reason+" ", width)) + "\n"
}

// reviewStaleView says the worktree has moved since the diff on screen was read.
func (m model) reviewStaleView() string {
	// The editor holds the stage rather than the diff, and the move it reports is usually the
	// reader's own writing, so there is nothing here worth a row.
	if !m.review.diff.moved || m.review.diff.pinned || m.review.pass.atEditor {
		return "\n"
	}

	return paint.Panel.Style(reviewMovedTone).Render(m.reviewStaleSaid()) + "\n"
}

// reviewStaleSaid is why the diff is behind the worktree, in the terms the reader can act on.
func (m model) reviewStaleSaid() string {
	if m.isReviewAgentWriting() {
		return "the agent is writing · the diff catches up when it stops"
	}

	if m.doesReviewFollowLive() {
		return "the worktree has moved · catching up"
	}

	if m.review.threads.holdsUnsent() {
		return "the worktree has moved · your comments are pinned to this round · " +
			keyLabel(m.keys.Review.Reload) + " to catch up"
	}

	return "the worktree has moved · " + keyLabel(m.keys.Review.Reload) + " to catch up"
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

// reviewScopeView says what the diff on screen is measured between, in the reader's own terms
// rather than as rounds, and draws the other thing the base key could show beside it.
func (m model) reviewScopeView() string {
	if m.review.diff.view == nil {
		return ""
	}

	from := "all changes"
	if m.review.diff.baseLabel != "" {
		from = m.review.diff.baseLabel
	}

	out := paint.Panel.Style(reviewScopeOnTone).Bold(true).Render(from)

	if m.doesReviewFitBaseLegend() {
		out += dimStyle.Render(" · " + m.reviewStepSaid())
	}

	return out + "\n"
}

// reviewStepSaid is the way through the rounds, naming only the direction that has somewhere to
// go.
func (m model) reviewStepSaid() string {
	back := keyLabel(m.keys.Review.Base)
	forward := keyLabel(m.keys.Review.BaseForward)

	switch {
	case m.review.diff.chapter < 0:
		return back + " for the latest round"
	case m.review.diff.chapter == 0:
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

// reviewBandRow draws one row of the band as a coloured state and a hint after it, cutting the
// hint rather than letting the row run past the pane.
func (m model) reviewBandRow(tone paint.Tone, state, hint string) string {
	style := paint.Panel.Style(tone).Bold(true)

	room := m.lineWidth() - lipgloss.Width(state) - len(" · ")
	if room <= 0 {
		return style.Render(paint.Clip(state, m.lineWidth())) + "\n"
	}

	return style.Render(state) + dimStyle.Render(" · "+paint.Clip(hint, room)) + "\n"
}

// reviewColumnStyle is the markdown a notice standing in for the patch is drawn in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
