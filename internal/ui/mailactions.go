package ui

import (
	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

// mailActions is the mailbox screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func mailActions() actionSpec {
	return actionSpec{
		table: []action{
			{
				id:   actionJump,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
				run:  model.jumpMail,
			},
			{
				id:   actionFilterOpen,
				keys: func(k config.Keybindings) []string { return k.Filter.Open },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Mail.Quit },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
			},
			{
				id:   actionFilterClear,
				keys: func(k config.Keybindings) []string { return k.Mail.Back },
				when: func(m model) bool { return m.filter.isStanding() },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
			},
			{
				id:   actionMailBack,
				keys: func(k config.Keybindings) []string { return k.Mail.Back },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.leaveMail() },
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
				run:  model.moveMail,
			},
		},
		legend: []legendRow{
			{
				of:    []actionID{actionMailBack},
				desc:  func(model) string { return "back" },
				strip: true,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: model.filterDesc,
			},
			{
				of:    []actionID{actionMove},
				desc:  func(model) string { return "move" },
				label: moveLabel,
			},
			{
				of:    []actionID{actionJump},
				desc:  func(model) string { return "top/bottom" },
				label: jumpLabel,
			},
			{
				of:    []actionID{actionFilterClear},
				desc:  func(model) string { return "clear search" },
				strip: true,
			},
		},
	}
}

// jumpMail answers the bottom key, the top key being a doubled gesture the screen has already
// taken before the table is reached.
func (m model) jumpMail(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		return m.jumpMailCursor(len(m.mailRows()) - 1), nil
	}

	return m, nil
}

func (m model) moveMail(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		return m.moveMailCursor(1), nil
	}

	return m.moveMailCursor(-1), nil
}

func jumpLabel(k config.Keybindings) string {
	return doubledLabel(k.Nav.Top) + "/" + keyLabel(k.Nav.Bottom)
}
