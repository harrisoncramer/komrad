package ui

import tea "charm.land/bubbletea/v2"

func (m *model) routeClick(click tea.MouseClickMsg) tea.Cmd {
	if click.Button != tea.MouseLeft || m.form.isActive() || m.writes.held.active {
		return nil
	}

	lister, ok := m.active().(activeLister)
	if !ok {
		return nil
	}

	hit, ok := m.findRowAt(click.X, click.Y)
	if !ok {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}

	return tea.Batch(m.enterStop(stopList), lister.activeList().click(m, hit.key, hit.nth))
}
