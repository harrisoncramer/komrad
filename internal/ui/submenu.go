package ui

import (
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// nestedMark follows an entry that opens a menu of its own.
const nestedMark = " " + glyph.Crumb

// submenuLevel is one menu of an open stack, and where its cursor sits.
type submenuLevel struct {
	menu   menu.Menu
	choice *int
}

func newSubmenuLevel(open menu.Menu) submenuLevel {
	choice := open.FirstOn()

	return submenuLevel{menu: open, choice: &choice}
}

// submenuState is the form for the menu on top of a stack, the menus under it rendered to its left.
func submenuState(parents []submenuLevel, level submenuLevel) formState {
	stack := slices.Concat(parents, []submenuLevel{level})
	state := formState{
		Form: form.Form{
			Build: func(form.Inputs) (*huh.Form, []string) {
				return submenuForm(level, len(parents) > 0, fixedKeys()), nil
			},
			Narrow:          true,
			ClosesOnRefusal: true,
		},
		isMenu: true,
		crumbs: append(xslice.Map(parents, func(p submenuLevel) string { return p.menu.Title }), level.menu.Title),
		update: func(m *model, msg tea.Msg) tea.Cmd {
			return m.updateSubmenu(msg, parents, level)
		},
		answer: func(m *model) tea.Cmd {
			if level.menu.IsList() {
				return m.openListActions(stack, level)
			}

			if *level.choice == menu.Cancel {
				return m.backSubmenu(parents)
			}

			entry := level.menu.Entries[*level.choice]
			if entry.OffSaid != "" {
				return nil
			}

			switch {
			case entry.Opens != nil:
				return m.pushSubmenu(stack, newSubmenuLevel(*entry.Opens))
			case entry.Form != nil:
				return m.stackBeside(parents, stack, level, hostedForm(entry.Form()))
			case entry.Pick == nil:
				return m.closeForm()
			}

			switch entry.After {
			case menu.Leave:
				m.leaveForm()
			case menu.Hold:
				return holdSubmit(func(*model) tea.Cmd { return entry.Pick() })(m)
			}

			return entry.Pick()
		},
		leading: func(width, _ int) []string {
			return submenuLeading(parents, level, width)
		},
	}
	if level.menu.Preview != nil {
		state.trailing = func(width, height int) string {
			return level.menu.Preview(*level.choice, width, height)
		}
	}

	return state
}

// stackBeside renders a form an entry opens to the right of the menu, or in the menu's place when
// the menu is in place.
func (m *model) stackBeside(parents, stack []submenuLevel, level submenuLevel, form formState) tea.Cmd {
	if level.menu.InPlace {
		return m.stackForm(parents, stack, form)
	}

	return m.stackForm(stack, stack, form)
}

// pushSubmenu renders a menu to the right of the one that opened it.
func (m *model) pushSubmenu(parents []submenuLevel, level submenuLevel) tea.Cmd {
	return m.openForm(submenuState(parents, level))
}

// openListActions renders the menu on the entry under a list's cursor in place of the list's
// preview.
func (m *model) openListActions(stack []submenuLevel, list submenuLevel) tea.Cmd {
	actions := list.menu.Actions(*list.choice)
	actions.InPlace = true

	return m.pushSubmenu(stack, newSubmenuLevel(actions))
}

// stackForm renders a form to the right of the menus beside it, going back to the menus that
// opened it.
func (m *model) stackForm(rendered, from []submenuLevel, form formState) tea.Cmd {
	form.crumbs = append(xslice.Map(from, func(l submenuLevel) string { return l.menu.Title }), form.crumbs...)
	form.leading = func(width, _ int) []string {
		return frozenMenus(rendered, width-formColumnFloor)
	}
	form.back = func(m *model) tea.Cmd {
		return m.backSubmenu(from)
	}

	return m.openForm(form)
}

// backSubmenu closes the top menu and hands the keys to the menu that opened it.
func (m *model) backSubmenu(parents []submenuLevel) tea.Cmd {
	if len(parents) == 0 {
		return m.closeForm()
	}

	last := len(parents) - 1

	return m.pushSubmenu(parents[:last], parents[last])
}

func submenuForm(level submenuLevel, nested bool, keys config.FixedKeys) *huh.Form {
	field := huh.NewSelect[int]().
		Key("submenu").
		Title(level.menu.Title).
		Value(level.choice).
		Options(submenuOptions(level.menu, nested)...)

	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(!level.menu.IsList()).WithKeyMap(submenuKeyMap(keys)).WithTheme(paint.Panel.FormTheme())
}

// submenuKeyGap is the columns between an entry's label and its key.
const submenuKeyGap = 4

// submenuKeyMap is the form's own keys without the select's filter.
func submenuKeyMap(keys config.FixedKeys) *huh.KeyMap {
	km := form.KeyMap(keys)
	km.Select.Filter.SetEnabled(false)

	return km
}

// updateSubmenu answers an entry by its own key, goes back a menu on the back key, and hands huh
// every other message.
func (m *model) updateSubmenu(msg tea.Msg, parents []submenuLevel, level submenuLevel) tea.Cmd {
	if click, ok := msg.(submenuClickMsg); ok {
		return m.answerSubmenuClick(click, parents, level)
	}

	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateForm(msg)
	}

	if len(parents) > 0 && keys.IsBound(press.String(), fixedKeys().Back) {
		return m.backSubmenu(parents)
	}

	if level.menu.IsList() && keys.IsBound(press.String(), m.keys.Menu.Open) {
		return m.openListActions(slices.Concat(parents, []submenuLevel{level}), level)
	}

	for i, entry := range level.menu.Entries {
		if keys.IsBound(press.String(), entry.Keys) {
			*level.choice = i

			return m.answerForm()
		}
	}

	cmd := m.updateForm(msg)
	if !level.isOnDivider() {
		return cmd
	}

	return tea.Batch(cmd, m.updateForm(msg))
}

func (l submenuLevel) isOnDivider() bool {
	choice := *l.choice

	return choice >= 0 && choice < len(l.menu.Entries) && l.menu.Entries[choice].Divider
}

type submenuClickMsg struct {
	parent bool
	at     int
}

func (m *model) clickSubmenu(key string, parent bool) tea.Cmd {
	at, err := strconv.Atoi(key)
	if err != nil || !m.form.isActive() {
		return nil
	}

	return m.form.update(m, submenuClickMsg{parent: parent, at: at})
}

func (m *model) answerSubmenuClick(click submenuClickMsg, parents []submenuLevel, level submenuLevel) tea.Cmd {
	if !click.parent {
		*level.choice = click.at

		return m.answerForm()
	}

	if click.at >= len(parents) {
		return nil
	}

	return m.pushSubmenu(parents[:click.at], parents[click.at])
}
