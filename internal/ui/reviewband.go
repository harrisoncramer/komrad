package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/dates"

	"charm.land/glamour/v2/ansi"
)

// reviewScopeOnTone is the half of the scope switch that is in force.
const reviewScopeOnTone = paint.TonePrimary

// bandView is what stands over the diff: whose work is being read, the round it is frozen on, and
// the pattern it is searched for.
func (r reviewScreen) bandView(m *model) string {
	return paint.Panel.Title("Review"+separated+r.agentName) + "\n" +
		r.frozenView(m) +
		r.diff.filter.view(m.glyphs) + diffMatchCount(&r.diff.Pane, r.diff.filter)
}

func (r reviewScreen) frozenView(m *model) string {
	if !r.diff.pinned || r.round == nil {
		return ""
	}

	said := fmt.Sprintf("Frozen on round %d from %s until you send your review%s%s shows the latest",
		r.round.GetOrdinal(), label.Friendly(dates.Parse(r.round.GetCreatedAt())),
		separated, m.menuChordLabel(m.keys.Review.Reload))

	var out strings.Builder
	for _, row := range paint.WrapRows(said, r.measureBandWidth(m)) {
		out.WriteString(paint.Panel.Style(reviewFrozenTone).Render(row) + "\n")
	}

	return out.String()
}

func (r reviewScreen) measureBandWidth(m *model) int {
	if split, ok := m.columnGeometry(); ok {
		return split.column
	}

	return lineWidthOf(m.paneWidth())
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
