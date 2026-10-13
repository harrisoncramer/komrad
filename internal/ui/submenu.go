package ui

import (
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// submenuCancel is the choice of the cancel entry every submenu ends on.
const submenuCancel = -1

// nestedMark is rendered after an entry that opens a menu of its own.
const nestedMark = " " + glyph.Crumb

// submenu is a short list rendered beside the list it was opened over, each entry picked by its
// own key as well as by the cursor.
type submenu struct {
	kind    formKind
	title   string
	entries []submenuEntry
	// actions is the menu the menu key opens on the entry under a list's cursor, nil for a menu
	// whose entries act on their own.
	actions func(choice int) submenu
	// preview renders the entry under a list's cursor beside the list, within width and height.
	preview func(choice, width, height int) string
	// empty is the row a list renders while it holds nothing.
	empty string
	// inPlace says a form an entry opens takes the menu's place rather than opening beside it.
	inPlace bool
}

func (s submenu) isList() bool {
	return s.actions != nil
}

// submenuEntry is one row of a submenu.
type submenuEntry struct {
	label string
	keys  []string
	// isOff says the entry would do nothing here.
	isOff bool
	// isDivider says the row is a rule between two bands of entries.
	isDivider bool
	// opens is the menu the entry opens to the right of its own.
	opens *submenu
	// form is the form the entry opens to the right of its own menu.
	form func() formState
	// write is what the entry does, the menu held up while a write it began is in flight.
	write func(m *model) tea.Cmd
	// open leaves the menu for the form or screen the entry goes on to.
	open func(m *model) tea.Cmd
}

// writeEntry is an entry whose write the root runs.
func writeEntry(label string, keys []string, run func(*model) tea.Cmd) submenuEntry {
	return submenuEntry{
		label: label,
		keys:  keys,
		write: run,
	}
}

// openEntry is an entry that leaves the menu for the form or screen run opens.
func openEntry(label string, keys []string, run func(*model) tea.Cmd) submenuEntry {
	return submenuEntry{
		label: label,
		keys:  keys,
		open: func(m *model) tea.Cmd {
			m.leaveForm()
			return run(m)
		},
	}
}

// submenuLevel is one menu of an open stack, and where its cursor sits.
type submenuLevel struct {
	menu   submenu
	choice *int
}

// nestedEntry is an entry that opens a menu, gray when nothing inside it would do anything.
func nestedEntry(label string, keys []string, menu submenu) submenuEntry {
	return submenuEntry{
		label: label,
		keys:  keys,
		isOff: !menu.isList() && menu.firstOn() == submenuCancel,
		opens: &menu,
	}
}

// withNested is a band with a nested menu at its foot.
func withNested(menu submenu, label string, keys []string, nested submenu) submenu {
	at := len(menu.entries)
	menu.entries = append(menu.entries[:at:at], nestedEntry(label, keys, nested))

	return menu
}

var dividerEntry = submenuEntry{isOff: true, isDivider: true}

// openSubmenu renders a submenu, its cursor on the first entry that would do something.
func openSubmenu(m *model, menu submenu) tea.Cmd {
	return m.openForm(submenuState(nil, newSubmenuLevel(menu)))
}

func newSubmenuLevel(menu submenu) submenuLevel {
	choice := menu.firstOn()

	return submenuLevel{menu: menu, choice: &choice}
}

// submenuState is the form for the menu on top of a stack, the menus under it rendered to its left.
func submenuState(parents []submenuLevel, level submenuLevel) formState {
	state := formState{
		kind:   level.menu.kind,
		isMenu: true,
		crumbs: append(xslice.Map(parents, func(p submenuLevel) string { return p.menu.title }), level.menu.title),
		update: func(m *model, msg tea.Msg) tea.Cmd {
			return m.updateSubmenu(msg, parents, level)
		},
		submit: func(m *model) tea.Cmd {
			stack := slices.Concat(parents, []submenuLevel{level})
			if level.menu.isList() {
				return m.openListActions(stack, level)
			}

			if *level.choice == submenuCancel {
				return m.backSubmenu(parents)
			}

			entry := level.menu.entries[*level.choice]

			switch {
			case entry.opens != nil:
				return m.pushSubmenu(stack, newSubmenuLevel(*entry.opens))
			case entry.form != nil && level.menu.inPlace:
				return m.stackForm(parents, stack, entry.form())
			case entry.form != nil:
				return m.stackForm(stack, stack, entry.form())
			case entry.write != nil:
				return holdSubmit(entry.write)(m)
			case entry.open != nil:
				return entry.open(m)
			}

			return m.closeForm()
		},
		rebuild: func(*model) (*huh.Form, []string) {
			return submenuForm(level, len(parents) > 0, fixedKeys()), nil
		},
		leading: func(width, _ int) []string {
			return submenuLeading(parents, level, width)
		},
	}
	if level.menu.preview != nil {
		state.trailing = func(width, height int) string {
			return level.menu.preview(*level.choice, width, height)
		}
	}

	return state
}

// pushSubmenu renders a menu to the right of the one it was opened from.
func (m *model) pushSubmenu(parents []submenuLevel, level submenuLevel) tea.Cmd {
	return m.openForm(submenuState(parents, level))
}

// openListActions renders the menu on the entry under a list's cursor in place of what the list
// previews.
func (m *model) openListActions(stack []submenuLevel, list submenuLevel) tea.Cmd {
	actions := list.menu.actions(*list.choice)
	actions.inPlace = true

	return m.pushSubmenu(stack, newSubmenuLevel(actions))
}

// stackForm renders a form to the right of the menus rendered beside it, going back to the menus it
// was opened from.
func (m *model) stackForm(rendered, from []submenuLevel, form formState) tea.Cmd {
	form.crumbs = append(xslice.Map(from, func(l submenuLevel) string { return l.menu.title }), form.crumbs...)
	form.leading = func(width, _ int) []string {
		return frozenMenus(rendered, width-formColumnFloor)
	}
	form.back = func(m *model) tea.Cmd {
		return m.backSubmenu(from)
	}

	return m.openForm(form)
}

// backSubmenu closes the menu on top, handing the keys to the one it was opened from.
func (m *model) backSubmenu(parents []submenuLevel) tea.Cmd {
	if len(parents) == 0 {
		return m.closeForm()
	}

	last := len(parents) - 1

	return m.pushSubmenu(parents[:last], parents[last])
}

func (s submenu) firstOn() int {
	for i, entry := range s.entries {
		if !entry.isOff {
			return i
		}
	}

	return submenuCancel
}

func submenuForm(level submenuLevel, nested bool, keys config.FixedKeys) *huh.Form {
	field := huh.NewSelect[int]().
		Key("submenu").
		Title(level.menu.title).
		Value(level.choice).
		Options(submenuOptions(level.menu, nested)...)

	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(!level.menu.isList()).WithKeyMap(submenuKeyMap(keys)).WithTheme(paint.Panel.FormTheme())
}

// submenuOptions is a menu's entries ending on the row that leaves it, or a list's entries alone.
func submenuOptions(menu submenu, nested bool) []huh.Option[int] {
	if menu.isList() {
		if len(menu.entries) == 0 {
			return []huh.Option[int]{huh.NewOption(paint.Panel.Style(paint.ToneMuted).Render(menu.empty), submenuCancel)}
		}

		options := make([]huh.Option[int], 0, len(menu.entries))
		for i, entry := range menu.entries {
			options = append(options, huh.NewOption(markEntry(entry.label, i), i))
		}

		return options
	}

	labelCols, keyCols := submenuColumns(menu.entries)

	options := make([]huh.Option[int], 0, len(menu.entries)+1)
	for i, entry := range menu.entries {
		label := submenuLabel(entry, labelCols, keyCols)
		if !entry.isOff {
			label = markEntry(label, i)
		}

		options = append(options, huh.NewOption(label, i))
	}

	last := "cancel"
	if nested {
		last = "back"
	}

	return append(options, huh.NewOption(markEntry(last, submenuCancel), submenuCancel))
}

// submenuKeyGap is the columns between an entry's label and its key.
const submenuKeyGap = 4

// submenuKeyMap is the form's own keys without the select's filter.
func submenuKeyMap(keys config.FixedKeys) *huh.KeyMap {
	km := multiFieldKeyMap(keys)
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

	if len(parents) > 0 && isBound(press.String(), fixedKeys().Back) {
		return m.backSubmenu(parents)
	}

	if level.menu.isList() && isBound(press.String(), m.keys.Menu.Open) {
		return m.openListActions(slices.Concat(parents, []submenuLevel{level}), level)
	}

	for i, entry := range level.menu.entries {
		if isBound(press.String(), entry.keys) {
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

	return choice >= 0 && choice < len(l.menu.entries) && l.menu.entries[choice].isDivider
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
