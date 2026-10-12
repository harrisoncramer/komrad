package ui

import tea "charm.land/bubbletea/v2"

func (m *model) routeClick(click tea.MouseClickMsg) tea.Cmd {
	if click.Button != tea.MouseLeft || m.form.isActive() || m.writes.held.active {
		return nil
	}

	hit, ok := m.findRowAt(click.X, click.Y)
	if !ok {
		return nil
	}

	switch hit.section {
	case sectionViews:
		return m.clickView(hit.key)
	case sectionKinds:
		return m.clickKind(hit.key)
	case sectionRecord:
		return m.clickList(stopRecord, m.detailsList(), hit)
	case sectionList:
		if lister, ok := m.active().(activeLister); ok {
			return m.clickList(stopList, lister.activeList(), hit)
		}
	case sectionTabs:
	}

	return nil
}

func (m *model) clickList(to stop, list keyedList, hit rowHit) tea.Cmd {
	if m.isViewsOpen() {
		m.closeViews()
	}

	return tea.Batch(m.enterStop(to), list.click(m, hit.key, hit.nth))
}
