package ui

import tea "charm.land/bubbletea/v2"

type rowClicker interface {
	clickRow(m *model, key string, nth int) tea.Cmd
}

func (m *model) routeClick(click tea.MouseClickMsg) tea.Cmd {
	if click.Button != tea.MouseLeft || m.form.isActive() || m.writes.held.active {
		return nil
	}

	list, ok := m.active().(rowClicker)
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

	return tea.Batch(m.enterStop(stopList), list.clickRow(m, hit.key, hit.nth))
}
