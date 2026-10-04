package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

type checksScreen struct {
	rowCursor
	agentID string
	from    screen
}

type checkRow struct {
	name string
}

func (r checkRow) key() string {
	return "check:" + r.name
}

func (c *checksScreen) enter(_ *model, target screenTarget) tea.Cmd {
	*c = checksScreen{
		agentID: target.agentID,
		from:    target.from,
	}

	return nil
}

func (c *checksScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *checksScreen) list() screenList[checkRow] {
	return screenList[checkRow]{
		cursor: func() *rowCursor { return &c.rowCursor },
		rows:   c.rows,
		name:   checkRowName,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
		quit:   func(k config.Keybindings) []string { return k.Checks.Quit },
		back:   func(k config.Keybindings) []string { return k.Checks.Back },
	}
}

func (c *checksScreen) rows(*model) []checkRow {
	return nil
}

func checkRowName(row checkRow) string {
	return row.name
}

func (c *checksScreen) view(m *model) string {
	return m.columnListView(screenListParams{
		help:  c.help,
		empty: checksEmptyLine,
		rows:  len(c.rows(m)),
		lines: func(listPane) ([]string, int) { return c.lines(m), c.cursor },
	})
}

const checksEmptyLine = "no checks yet"

func (c *checksScreen) lines(m *model) []string {
	return []string{dimStyle().Render("  " + emptyLineOf(m.filter, checksEmptyLine))}
}

func (c *checksScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table: append(c.list().actions(),
			screenAction{
				id:   actionChecksBack,
				keys: func(k config.Keybindings) []string { return k.Checks.Back },
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(c.from, screenTarget{agentID: c.agentID})
				},
			},
		),
		legend: slices.Concat(screenNavLegend(), []screenLegendRow{
			{
				of:    []actionID{actionChecksBack},
				desc:  func(*model) string { return "back" },
				strip: true,
			},
		}),
	}
}

func (c *checksScreen) help(m *model) keyMap {
	return c.listActions().help(m)
}

func (c *checksScreen) parent() screen {
	return c.from
}

func (c *checksScreen) crumbs(m *model) crumbs {
	name := c.agentID
	if item, ok := m.listing.anyAgent(c.agentID); ok {
		name = m.listing.agentName(item.Agent)
	}

	return crumbs{trail: []string{name, "checks"}}
}

func (c *checksScreen) drawsSwitcher() bool {
	return true
}

func (c *checksScreen) doesDrawFromListing() bool {
	return true
}

func (c *checksScreen) takesEveryKey() bool {
	return false
}

func (c *checksScreen) column() columnSpec {
	return columnSpec{
		floor:   columnFloor,
		docRows: func(*model, int) []string { return nil },
	}
}

func (c *checksScreen) stageResting(*model) tea.Cmd {
	return nil
}

func (c *checksScreen) dispatches(*model) []dispatching {
	return []dispatching{c.listActions()}
}

var (
	_ listFilterer = (*checksScreen)(nil)
	_ crumbDrawer  = (*checksScreen)(nil)
)

func (c *checksScreen) hoveredKey(m *model) string {
	return c.list().hovered(m)
}

func (c *checksScreen) placeAt(m *model, key string) {
	c.list().place(m, key)
}

func (c *checksScreen) filterRows(m *model) []filterRow {
	return c.list().filterRows(m)
}
