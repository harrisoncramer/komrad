package ui

import (
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

// crumbs is the trail from a view down to the page open under it, the last step lit.
type crumbs struct {
	trail []string
	// tally is said after the trail, dimmed.
	tally string
}

// crumbed is a page opened from under a view, drawing the trail back to it.
type crumbed interface {
	// parent is the view the page was opened from.
	parent() screen

	// crumbs is the trail under the parent, down to the page.
	crumbs(s panelShared) crumbs
}

func (c crumbs) view(palette paint.Palette, width int) string {
	if len(c.trail) == 0 {
		return ""
	}

	muted := palette.Style(paint.ToneMuted)
	steps := make([]string, len(c.trail))
	for i, step := range c.trail {
		steps[i] = muted.Render(step)
	}
	steps[len(steps)-1] = palette.Style(paint.ToneAccent).Render(c.trail[len(c.trail)-1])

	line := strings.Join(steps, muted.Render(" "+glyph.Crumb+" "))
	if c.tally != "" {
		line += muted.Render(separated + c.tally)
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}

// activeCrumbs is the trail from the view the page that is up was opened from, and false on a
// view of its own.
func (m *model) activeCrumbs() (crumbs, screen, bool) {
	parent, trail, ok := m.pageCrumbs()
	if !ok {
		return crumbs{}, m.screen, false
	}

	if i := slices.IndexFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == parent }); i >= 0 {
		trail.trail = slices.Concat([]string{viewEntries()[i].label}, trail.trail)
	}

	return trail, parent, true
}

// pageCrumbs is the trail under the view, from the page that is up or from the form open over a
// view.
func (m *model) pageCrumbs() (screen, crumbs, bool) {
	if c, ok := m.active().(crumbed); ok {
		return c.parent(), c.crumbs(m.shared()), true
	}

	if m.form.isOpening() && m.form.crumb != "" {
		return m.screen, crumbs{trail: []string{m.form.crumb}}, true
	}

	return m.screen, crumbs{}, false
}
