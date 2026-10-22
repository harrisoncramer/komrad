package ui

import (
	"slices"

	"komrad/internal/ui/screen"
)

// activeCrumbs is the trail from the view the screen that is up sits under, that view, and
// whether a page or a form is open under it.
func (m *model) activeCrumbs() (screen.Crumbs, screen.ID, bool) {
	parent, trail, opened := m.pageCrumbs()
	if !opened {
		trail = m.hoveredCrumbs()
	}

	if i := slices.IndexFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == parent }); i >= 0 {
		trail.Trail = slices.Concat([]string{viewEntries()[i].label}, trail.Trail)
	}

	return trail, parent, opened
}

// pageCrumbs is the trail under the view, from the page that is up or from the form open over a
// view.
func (m *model) pageCrumbs() (screen.ID, screen.Crumbs, bool) {
	if c, ok := m.active().(crumbRenderer); ok {
		return c.parent(), c.crumbs(m.inputs()), true
	}

	if m.form.isOpening() && len(m.form.crumbs) > 0 {
		return m.screen, screen.Crumbs{Trail: m.form.crumbs}, true
	}

	return m.screen, screen.Crumbs{}, false
}

func (m *model) hoveredCrumbs() screen.Crumbs {
	h, ok := m.active().(hoveredNamer)
	if !ok {
		return screen.Crumbs{}
	}

	name := h.hoveredName(m.inputs())
	if name == "" {
		return screen.Crumbs{}
	}

	return screen.Crumbs{Trail: []string{name}}
}
