package ui

import (
	"fmt"
	"strings"

	"komrad/internal/git"
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
	if !r.isFrozen() {
		return ""
	}

	snapshot := git.ShortSHA(r.round.GetSnapshotSha())
	if at := label.Friendly(dates.Parse(r.round.GetCreatedAt())); at != "" {
		snapshot += ", taken " + at
	}
	said := fmt.Sprintf("frozen on round %d (%s) until you send your review", r.round.GetOrdinal(), snapshot)

	style := dimStyle()
	if behind := r.behindSaid(m); behind != "" {
		said += separated + behind + separated + m.menuChordLabel(m.keys.Review.Reload) + " shows the current diff"
		style = paint.Panel.Style(reviewFrozenTone)
	}

	var out strings.Builder
	for _, row := range paint.WrapRows(said, r.bandWidth(m)) {
		out.WriteString(style.Render(row) + "\n")
	}

	return out.String()
}

func (r reviewScreen) isFrozen() bool {
	return r.diff.pinned && r.round != nil && r.diff.spec.answer == nil && r.diff.spec.origin == nil
}

func (r reviewScreen) behindSaid(m *model) string {
	newest := r.round
	if len(r.diff.rounds) > 0 {
		newest = r.diff.rounds[len(r.diff.rounds)-1]
	}

	roundsBehind := int(newest.GetOrdinal() - r.round.GetOrdinal())
	movedSince, _ := r.findTaskMove(m)
	moved := movedSince != ""

	var parts []string
	switch {
	case roundsBehind > 0 && moved:
		parts = append(parts, label.Counted(roundsBehind, "round")+" behind, with more written since")
	case roundsBehind > 0:
		parts = append(parts, label.Counted(roundsBehind, "round")+" behind")
	case moved:
		parts = append(parts, "the worktree has moved since")
	}

	if newest.GetBaseSha() != r.round.GetBaseSha() {
		parts = append(parts, fmt.Sprintf("rebased since, from %s onto %s",
			git.ShortSHA(r.round.GetBaseSha()), git.ShortSHA(newest.GetBaseSha())))
	}

	return strings.Join(parts, separated)
}

func (r reviewScreen) bandWidth(m *model) int {
	if split, ok := m.columnGeometry(); ok {
		return split.column
	}

	return lineWidthOf(m.paneWidth())
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
