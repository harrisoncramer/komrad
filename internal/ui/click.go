package ui

import tea "charm.land/bubbletea/v2"

type clicker interface {
	click(m *model, key string, nth int) tea.Cmd
}

type clickFunc func(m *model, key string, nth int) tea.Cmd

func (f clickFunc) click(m *model, key string, nth int) tea.Cmd {
	return f(m, key, nth)
}

func (m *model) routeClick(click tea.MouseClickMsg) tea.Cmd {
	if click.Button != tea.MouseLeft || m.form.isActive() || m.writes.held.active {
		return nil
	}

	if hit, ok := m.findKeyAt(click.X, click.Y); ok {
		if c, ok := m.clickerOf(hit.zone); ok {
			return c.click(m, hit.key, hit.nth)
		}
	}

	return m.clickBlock(click.X, click.Y)
}

func (m *model) clickerOf(name zone) (clicker, bool) {
	switch name {
	case zoneViews:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickView(key) }), true
	case zoneKinds:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickKind(key) }), true
	case zoneTabs:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickTab(key) }), true
	case zoneDetails:
		return m.listClicker(stopRecord, m.detailsList()), true
	case zoneList:
		if lister, ok := m.active().(activeLister); ok {
			return m.listClicker(stopList, lister.activeList()), true
		}
	}

	return nil, false
}

func (m *model) listClicker(to stop, list keyedList) clicker {
	return clickFunc(func(m *model, key string, nth int) tea.Cmd {
		if m.isViewsOpen() {
			m.closeViews()
		}

		return tea.Batch(m.enterStop(to), list.click(m, key, nth))
	})
}

func (m *model) clickBlock(x, y int) tea.Cmd {
	if m.isViewsOpen() {
		m.closeViews()
	}

	return m.focusBlockIn(m.findSidebarBlocks(), x, y)
}
