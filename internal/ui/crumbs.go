package ui

import (
	"slices"

	"komrad/internal/ui/screen"
)

// crumbs is the trail from a view down to the row or page the cursor is on.
type crumbs struct {
	trail []string
	// tally is said after the trail.
	tally string
}

// activeCrumbs is the trail from the view the screen that is up sits under, that view, and
// whether a page or a form is open under it.
func (m *model) activeCrumbs() (crumbs, screen.ID, bool) {
	parent, trail, opened := m.pageCrumbs()
	if !opened {
		trail = m.hoveredCrumbs()
	}

	if i := slices.IndexFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == parent }); i >= 0 {
		trail.trail = slices.Concat([]string{viewEntries()[i].label}, trail.trail)
	}

	return trail, parent, opened
}

// pageCrumbs is the trail under the view, from the page that is up or from the form open over a
// view.
func (m *model) pageCrumbs() (screen.ID, crumbs, bool) {
	if c, ok := m.active().(crumbRenderer); ok {
		return c.parent(), c.crumbs(m), true
	}

	if m.form.isOpening() && len(m.form.crumbs) > 0 {
		return m.screen, crumbs{trail: m.form.crumbs}, true
	}

	return m.screen, crumbs{}, false
}

func (m *model) hoveredCrumbs() crumbs {
	h, ok := m.active().(hoveredNamer)
	if !ok {
		return crumbs{}
	}

	name := h.hoveredName(m)
	if name == "" {
		return crumbs{}
	}

	return crumbs{trail: []string{name}}
}
