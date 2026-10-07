package ui

import (
	"komrad/internal/ui/paint"

	"charm.land/glamour/v2/ansi"
)

// reviewScopeOnTone is the half of the scope switch that is in force.
const reviewScopeOnTone = paint.TonePrimary

// bandView is what stands over the diff: whose work is being read and the pattern it is searched for.
func (r reviewScreen) bandView(m *model) string {
	return paint.Panel.Title("Review"+separated+r.agentName) + "\n" +
		r.diff.filter.view(paint.Panel, m.glyphs) + diffMatchCount(&r.diff.Pane, r.diff.filter)
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
