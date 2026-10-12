package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

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

	hit, marked := m.findKeyAt(click.X, click.Y)
	if marked && hit.zone == zoneSearch && hit.key == strconv.Itoa(int(m.atStop())) && m.isSearchTyping() {
		return nil
	}

	settled := m.settleSearch()
	if marked {
		if c, ok := m.clickerOf(hit.zone); ok {
			return tea.Batch(settled, c.click(m, hit.key, hit.nth))
		}
	}

	return tea.Batch(settled, m.clickBlock(click.X, click.Y))
}

func (m *model) settleSearch() tea.Cmd {
	if !m.isSearchTyping() {
		return nil
	}

	return m.route(keyMsg("enter"))
}

func (m *model) clickerOf(name zone) (clicker, bool) {
	switch name {
	case zoneViews:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickView(key) }), true
	case zoneKinds:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickKind(key) }), true
	case zoneTabs:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickTab(key) }), true
	case zoneSearch:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickSearch(key) }), true
	case zoneMessages:
		return clickFunc(func(m *model, _ string, _ int) tea.Cmd { return m.clickMessages() }), true
	case zoneFiles:
		return m.stopClicker(stopBlock, clickFunc(func(m *model, path string, _ int) tea.Cmd { return m.review.clickFile(m, path) })), true
	case zoneDetails:
		return m.stopClicker(stopRecord, m.detailsList()), true
	case zoneList:
		if lister, ok := m.active().(activeLister); ok {
			return m.stopClicker(stopList, lister.activeList()), true
		}
	}

	return nil, false
}

func (m *model) stopClicker(to stop, c clicker) clicker {
	return clickFunc(func(m *model, key string, nth int) tea.Cmd {
		if m.isViewsOpen() {
			m.closeViews()
		}

		return tea.Batch(m.enterStop(to), c.click(m, key, nth))
	})
}

func (m *model) clickBlock(x, y int) tea.Cmd {
	if m.isViewsOpen() {
		m.closeViews()
	}

	return m.focusBlockIn(m.findSidebarBlocks(), x, y)
}
