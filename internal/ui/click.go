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

	if hit.section == sectionViews {
		return m.clickView(hit.key)
	}

	to, list, ok := m.clickedList(hit.section)
	if !ok {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}

	return tea.Batch(m.enterStop(to), list.click(m, hit.key, hit.nth))
}

func (m *model) clickedList(section sidebarSection) (stop, keyedList, bool) {
	switch section {
	case sectionRecord:
		return stopRecord, m.detailsList(), true
	case sectionList:
		if lister, ok := m.active().(activeLister); ok {
			return stopList, lister.activeList(), true
		}
	case sectionTabs, sectionViews:
		return 0, nil, false
	}

	return 0, nil, false
}
