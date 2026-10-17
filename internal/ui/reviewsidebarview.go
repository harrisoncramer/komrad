package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/charmbracelet/x/ansi"
)

// sideView is the rows over the file tree cut to lines rows, windowed on the row the cursor is on.
func (r reviewScreen) sideView(m *model, lines int) string {
	sideRows := r.sideRows(m)
	if len(sideRows) == 0 || lines == 0 {
		return ""
	}

	width := lineWidthOf(m.paneWidth())
	focused := r.isBlockFocused(m)
	rows := make([]string, 0, len(sideRows))
	keys := make([]string, 0, len(sideRows))
	cursor := 0
	for at, row := range sideRows {
		current := r.side != nil && *r.side == row.sideCursor && row.kind.isStop()
		if current {
			cursor = at
		}

		rows = append(rows, ansi.Truncate(r.sideRowView(m, row, current && focused), max(width, 0), "…"))
		keys = append(keys, row.key())
	}

	if mark := m.spanMarker(zoneSide, keys); mark != nil {
		for at, row := range rows {
			if sideRows[at].kind.isStop() {
				rows[at] = mark(at, row)
			}
		}
	}

	if lines < 0 || lines >= len(rows) {
		return strings.Join(rows, "\n") + "\n"
	}

	return paint.RenderList(rows, cursor, lines, dimStyle())
}

// sideRowView is one row over the file tree, lit while the cursor is on it.
func (r reviewScreen) sideRowView(m *model, row sideRow, lit bool) string {
	text := row.text
	switch row.kind {
	case sideBlank:
		return ""
	case sideHeading:
		return paint.Panel.Title(text)
	case sideTask, sideThisTask:
		if row.depth > 0 {
			text = strings.Repeat("  ", row.depth-1) + glyph.Child + " " + text
		}
		if row.kind == sideThisTask {
			text = paint.Panel.Style(reviewScopeOnTone).Render(text)
		}
	case sideRound:
		text = paint.Panel.Style(reviewScopeOnTone).Render(row.line) + " " + text
	case sideRoundsMore:
		text = paint.Panel.Style(reviewScopeOnTone).Render(row.line) + " " + dimStyle().Render(text)
	case sideWorktree, sideWaiting:
		text = dimStyle().Italic(true).Render(text)
	case sideDescription:
		mark := glyph.FolderClosed
		if r.openDescription {
			mark = glyph.FolderOpen
		}
		text = m.glyphs.Spell(mark) + " " + text
	case sideDescriptionLine:
		text = dimStyle().Render("  " + text)
	case sideForge, sideNeed:
	}

	if row.note != "" {
		text += "  " + dimStyle().Render(row.note)
	}

	prefix := "  "
	if lit {
		prefix = paint.Panel.Style(paint.TonePrimary).Bold(true).Render(glyph.Focus) + " "
	}

	return prefix + text
}

// verdictMark is the glyph a released review's verdict wears.
func verdictMark(m *model, verdict agentv1.ReviewVerdict) string {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return paint.Panel.Style(paint.ToneSuccess).Render(m.glyphs.Spell(glyph.Approved))
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return paint.Panel.Style(paint.ToneDanger).Render(m.glyphs.Spell(glyph.ChangesRequested))
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return dimStyle().Render(m.glyphs.Spell(glyph.ThreadRead))
}

// decisionSaid is the forge's review decision on the pull request, empty when it has none.
func decisionSaid(decision pullrequestv1.ReviewDecision) string {
	switch decision {
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED:
		return "approved"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED:
		return "changes requested"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING:
		return "review required"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED:
		return "review requested again"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_UNSPECIFIED, pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE:
	}

	return ""
}
