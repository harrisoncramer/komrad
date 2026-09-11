package ui

import (
	"fmt"
	"strings"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
)

// reviewBandFloor is how many rows the band leaves the rest of the frame: the blank row, the
// search prompt, and enough of the diff to be worth drawing.
const reviewBandFloor = 5

// reviewScopeOnStyle is the half of the scope switch that is in force.
var reviewScopeOnStyle = lipgloss.NewStyle().Foreground(paint.Accent).Bold(true)

// reviewBandView is what stands over the diff: what is being read, how big it is, what the
// reader has sent, and the pattern they are searching it for.
func (m model) reviewBandView(width int) string {
	// Explainer: the band is built as rows rather than one string so a short pane can drop the
	// ones it has no room for. What the reader is doing and what they are looking at are worth
	// more than the counts, so the counts go first and the mode line never goes at all.
	kept := m.reviewBandRows()

	out := strings.Join(kept, "") + "\n\n" + m.review.filter.view() + m.reviewMatchCount()

	if m.review.view != nil && m.review.view.Len() > 0 {
		out += m.reviewDeadView(width)
	}

	return out
}

// reviewBandRows is the band's rows in the order they give way, least valuable last.
func (m model) reviewBandRows() []string {
	rows := []string{
		paint.TitleOf(m.isReviewDiffFocused(), "Review · "+m.review.agentName) + "\n",
		m.reviewModeView(),
		m.reviewScopeView(),
		m.reviewStatView(),
		m.reviewStaleView(),
		m.reviewDeliveryCountView(),
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
	reason, dead := m.reviewIsReadOnly()
	if !dead {
		return ""
	}

	return reviewDeadStyle.Render(reviewFill(" "+reason+" ", width)) + "\n"
}

// reviewStaleView says the agent has written since the photograph on screen was taken, and holds
// its row when it has not, so the diff under it does not jump every time the agent saves.
func (m model) reviewStaleView() string {
	if !m.review.moved || m.review.pinned {
		return "\n"
	}

	said := "the agent has written since · " + keyLabel(m.keys.Review.Reload) + " to catch up"

	return reviewMovedStyle.Render(said) + "\n"
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

// reviewStatView is how big the change is, with the counts coloured the way the lines themselves
// are.
func (m model) reviewStatView() string {
	if m.review.view == nil {
		return ""
	}

	stat := m.review.view.Stat()
	if stat.Files == 0 {
		return ""
	}

	out := dimStyle.Render(counted(stat.Files, "file")+", ") +
		m.reviewBandStyle(reviewAddedStyle).Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		m.reviewBandStyle(reviewRemovedStyle).Render(fmt.Sprintf("-%d", stat.Removed))

	if read, total := m.review.view.CountViewedFiles(); read > 0 {
		out += dimStyle.Render(fmt.Sprintf("  %d/%d viewed", read, total))
	}

	return out + "\n"
}

// reviewScopeView says what the diff on screen is measured between, in the reader's own terms
// rather than as rounds, and draws the other thing the base key could show beside it.
func (m model) reviewScopeView() string {
	if m.review.view == nil {
		return ""
	}

	from := "all changes"
	if m.review.baseLabel != "" {
		from = m.review.baseLabel
	}

	out := m.reviewBandStyle(reviewScopeOnStyle).Render(from)

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
	case m.review.chapter < 0:
		return back + " for the latest round"
	case m.review.chapter == 0:
		return forward + " for a later round"
	default:
		return back + " back · " + forward + " forward"
	}
}

// reviewMatchCount is where in the search the cursor is standing, drawn under the pattern the
// panel's own filter is already showing.
func (m model) reviewMatchCount() string {
	if m.review.view == nil || !m.review.filter.isStanding() {
		return ""
	}

	where := counted(m.review.view.MatchCount(), "match")
	if at, ok := m.review.view.MatchNumber(m.review.cursor); ok {
		where = fmt.Sprintf("%d of %d", at, m.review.view.MatchCount())
	}

	// A match inside a file folded away is on no row, so stepping through the matches never
	// reaches it, and it is counted apart rather than left to look like nothing was found.
	if hidden := m.review.view.HiddenMatches(); hidden > 0 {
		where += fmt.Sprintf(" · %d in viewed files", hidden)
	}

	return dimStyle.Render("  "+where) + "\n\n"
}

// reviewBandRow draws one row of the band as a coloured state and a hint after it, cutting the
// hint rather than letting the row run past the pane.
func (m model) reviewBandRow(style lipgloss.Style, state, hint string) string {
	style = m.reviewBandStyle(style)

	room := m.lineWidth() - lipgloss.Width(state) - len(" · ")
	if room <= 0 {
		return style.Render(paint.Clip(state, m.lineWidth())) + "\n"
	}

	return style.Render(state) + dimStyle.Render(" · "+paint.Clip(hint, room)) + "\n"
}

// reviewBandStyle is what the band over the diff wears: its own colour while the diff holds the
// keys, the muted one while the sidebar does.
func (m model) reviewBandStyle(style lipgloss.Style) lipgloss.Style {
	if m.isReviewDiffFocused() {
		return style
	}

	return style.Foreground(paint.Muted)
}

// reviewColumnStyle is the markdown a notice standing in for the patch is drawn in.
func (m model) reviewColumnStyle() ansi.StyleConfig {
	if m.isReviewDiffFocused() {
		return paint.StageStyle
	}

	return paint.BlurredStageStyle
}
