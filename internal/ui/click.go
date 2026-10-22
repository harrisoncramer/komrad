package ui

import (
	"slices"
	"strconv"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/zone"

	tea "charm.land/bubbletea/v2"
)

type clicker interface {
	click(m *model, key string, nth int) tea.Cmd
}

type clickFunc func(m *model, key string, nth int) tea.Cmd

func (f clickFunc) click(m *model, key string, nth int) tea.Cmd {
	return f(m, key, nth)
}

type requestClick func(m *model, key string, nth int) screen.Request

func (f requestClick) click(m *model, key string, nth int) tea.Cmd {
	return m.apply(f(m, key, nth))
}

var formZones = []zone.Name{zoneMenu, zoneParentMenu}

func (m *model) routeClick(click tea.MouseClickMsg) tea.Cmd {
	if click.Button != tea.MouseLeft || m.writes.held.active {
		return nil
	}

	hit, marked := m.findKeyAt(click.X, click.Y)
	if m.form.isActive() && (!marked || !slices.Contains(formZones, hit.Zone)) {
		if !m.form.isMenu {
			return nil
		}
		if marked && m.isHoveredHit(hit) {
			return m.closeForm()
		}

		return tea.Batch(m.closeForm(), m.routeClick(click))
	}
	if marked && hit.Zone == zoneSearch && hit.Key == strconv.Itoa(int(m.atStop())) && m.isSearchTyping() {
		return nil
	}

	settled := m.settleSearch()
	if marked {
		if c, ok := m.clickerOf(hit.Zone); ok {
			return tea.Batch(settled, c.click(m, hit.Key, hit.Nth))
		}
	}

	return tea.Batch(settled, m.clickBlock(click.X, click.Y))
}

func (m *model) settleSearch() tea.Cmd {
	if !m.isSearchTyping() {
		return nil
	}

	return m.route(keys.Press("enter"))
}

func (m *model) clickerOf(name zone.Name) (clicker, bool) {
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
	case zoneMessage:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickMessage(key) }), true
	case zoneDoc:
		if slices.Contains(m.listStops(), stopBlock) {
			return m.stopClicker(stopBlock, clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickDocLine(key) })), true
		}
	case zoneLog:
		return m.stopClicker(stopBlock, clickFunc(func(m *model, key string, _ int) tea.Cmd {
			m.logs.clickLine(key)

			return nil
		})), true
	case zoneDiff:
		if m.screen == screen.Review {
			return m.stopClicker(stopList, clickFunc(func(m *model, key string, _ int) tea.Cmd {
				return m.apply(m.review.diffKeys().click(m.inputs(), key))
			})), true
		}
		if m.screen == screen.Documents {
			return m.stopClicker(stopBlock, clickFunc(func(m *model, key string, _ int) tea.Cmd {
				return m.apply(m.documents.diffKeys().click(m.inputs(), key))
			})), true
		}
	case zoneMenu:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickSubmenu(key, false) }), true
	case zoneParentMenu:
		return clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.clickSubmenu(key, true) }), true
	case zoneFiles:
		return m.stopClicker(stopBlock, clickFunc(func(m *model, path string, _ int) tea.Cmd { return m.apply(m.review.clickFile(m.inputs(), path)) })), true
	case zoneDetails:
		return m.stopClicker(stopRecord, requestClick(func(m *model, key string, nth int) screen.Request {
			return m.detailsList().click(m.inputs(), key, nth)
		})), true
	case zoneList:
		if lister, ok := m.active().(activeLister); ok {
			return m.listClicker(lister.activeList()), true
		}
	case zoneSide:
		return m.stopClicker(stopBlock, clickFunc(func(m *model, key string, _ int) tea.Cmd { return m.apply(m.review.clickSide(m.inputs(), key)) })), true
	case zoneColumn, zoneBlock:
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
