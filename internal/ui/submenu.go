package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// submenuCancel is the choice of the cancel entry every submenu ends on.
const submenuCancel = -1

// nestedMark is drawn after an entry that opens a menu of its own.
const nestedMark = " " + glyph.Crumb

// submenu is a short list drawn beside the list it was opened over, each entry picked by its
// own key as well as by the cursor.
type submenu struct {
	kind    formKind
	title   string
	crumb   string
	entries []submenuEntry
	// actions is the menu the menu key opens on the entry under a list's cursor, nil for a menu
	// whose entries act on their own.
	actions func(choice int) submenu
	// preview draws the entry under a list's cursor beside the list, within width and height.
	preview func(choice, width, height int) string
	// empty is the row a list draws while it holds nothing.
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
	write func(m model) (model, tea.Cmd)
	// open leaves the menu for the form or screen the entry goes on to.
	open func(m model) (tea.Model, tea.Cmd)
}

// writeEntry is an entry whose write is run through the host.
func writeEntry(label string, keys []string, run func(host) tea.Cmd) submenuEntry {
	return submenuEntry{
		label: label,
		keys:  keys,
		write: func(m model) (model, tea.Cmd) {
			cmd := run(&m)

			return m, cmd
		},
	}
}

// openEntry is an entry that leaves the menu for the form or screen run opens.
func openEntry(label string, keys []string, run func(host, panelShared) tea.Cmd) submenuEntry {
	return submenuEntry{
		label: label,
		keys:  keys,
		open: func(m model) (tea.Model, tea.Cmd) {
			m.leaveForm()
			cmd := run(&m, m.shared())

			return m, cmd
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

var dividerEntry = submenuEntry{isOff: true, isDivider: true}

// openSubmenu draws a submenu, its cursor on the first entry that would do something.
func openSubmenu(h host, menu submenu) tea.Cmd {
	return h.openForm(submenuState(nil, newSubmenuLevel(menu)))
}

func newSubmenuLevel(menu submenu) submenuLevel {
	choice := menu.firstOn()

	return submenuLevel{menu: menu, choice: &choice}
}

// submenuState is the form for the menu on top of a stack, the menus under it drawn to its left.
func submenuState(parents []submenuLevel, level submenuLevel) formState {
	state := formState{
		kind:   level.menu.kind,
		crumbs: append(xslice.Map(parents, func(p submenuLevel) string { return p.menu.crumb }), level.menu.crumb),
		update: func(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
			return m.updateSubmenu(msg, parents, level)
		},
		submit: func(m model) (tea.Model, tea.Cmd) {
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
		rebuild: func(model) (*huh.Form, []string) {
			return submenuForm(level, len(parents) > 0, formKeys()), nil
		},
		leading: func(int, int) []string {
			return frozenMenus(parents)
		},
	}
	if level.menu.preview != nil {
		state.trailing = func(width, height int) string {
			return level.menu.preview(*level.choice, width, height)
		}
	}

	return state
}

// pushSubmenu draws a menu to the right of the one it was opened from.
func (m model) pushSubmenu(parents []submenuLevel, level submenuLevel) (tea.Model, tea.Cmd) {
	cmd := m.openForm(submenuState(parents, level))

	return m, cmd
}

// openListActions draws the menu on the entry under a list's cursor in place of what the list
// previews.
func (m model) openListActions(stack []submenuLevel, list submenuLevel) (tea.Model, tea.Cmd) {
	actions := list.menu.actions(*list.choice)
	actions.inPlace = true

	return m.pushSubmenu(stack, newSubmenuLevel(actions))
}

// stackForm draws a form to the right of the menus drawn beside it, going back to the menus it
// was opened from.
func (m model) stackForm(drawn, from []submenuLevel, form formState) (tea.Model, tea.Cmd) {
	form.crumbs = append(xslice.Map(from, func(l submenuLevel) string { return l.menu.crumb }), form.crumbs...)
	form.leading = func(int, int) []string {
		return frozenMenus(drawn)
	}
	form.back = func(m model) (tea.Model, tea.Cmd) {
		return m.backSubmenu(from)
	}

	cmd := m.openForm(form)

	return m, cmd
}

// backSubmenu closes the menu on top, handing the keys to the one it was opened from.
func (m model) backSubmenu(parents []submenuLevel) (tea.Model, tea.Cmd) {
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

func submenuForm(level submenuLevel, nested bool, keys config.FormKeys) *huh.Form {
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
			options = append(options, huh.NewOption(entry.label, i))
		}

		return options
	}

	labelCols, keyCols := submenuColumns(menu.entries)

	options := make([]huh.Option[int], 0, len(menu.entries)+1)
	for i, entry := range menu.entries {
		options = append(options, huh.NewOption(submenuLabel(entry, labelCols, keyCols), i))
	}

	last := "cancel"
	if nested {
		last = "back"
	}

	return append(options, huh.NewOption(last, submenuCancel))
}

// frozenMenus is a stack of menus drawn gray.
func frozenMenus(levels []submenuLevel) []string {
	drawn := make([]string, 0, len(levels))
	for i, level := range levels {
		drawn = append(drawn, frozenSubmenu(level, i > 0))
	}

	return drawn
}

// frozenSubmenu is a menu as it is drawn once another has opened to its right.
func frozenSubmenu(level submenuLevel, nested bool) string {
	form := submenuForm(level, nested, formKeys()).WithShowHelp(false)
	form.Init()

	lines := strings.Split(ansi.Strip(form.View()), "\n")
	muted := paint.Panel.Style(paint.ToneMuted)

	return strings.Join(xslice.Map(lines, func(line string) string {
		return muted.Render(strings.TrimRight(line, " "))
	}), "\n")
}

// submenuColumns is the widest label and the widest key among a menu's entries.
func submenuColumns(entries []submenuEntry) (labelCols, keyCols int) {
	for _, entry := range entries {
		if entry.isDivider {
			continue
		}

		labelCols = max(labelCols, ansi.StringWidth(entryText(entry)))
		keyCols = max(keyCols, ansi.StringWidth(headLabel(entry.keys)))
	}

	return labelCols, keyCols
}

func entryText(entry submenuEntry) string {
	if entry.opens != nil {
		return entry.label + nestedMark
	}

	return entry.label
}

// submenuKeyGap is the columns between an entry's label and its key.
const submenuKeyGap = 4

// submenuLabel is an entry's label with the key that picks it right-aligned beside it, and gray
// when it would do nothing.
func submenuLabel(entry submenuEntry, labelCols, keyCols int) string {
	muted := paint.Panel.Style(paint.ToneMuted)
	width := labelCols + submenuKeyGap + keyCols

	if entry.isDivider {
		return muted.Render(strings.Repeat(glyph.Rule, width))
	}

	text := entryText(entry)
	key := headLabel(entry.keys)
	pad := strings.Repeat(" ", max(width-ansi.StringWidth(text)-ansi.StringWidth(key), 1))

	if entry.isOff {
		return muted.Render(text + pad + key)
	}

	return text + pad + muted.Render(key)
}

// submenuKeyMap is the form's own keys without the select's filter.
func submenuKeyMap(keys config.FormKeys) *huh.KeyMap {
	km := multiFieldKeyMap(keys)
	km.Select.Filter.SetEnabled(false)

	return km
}

// updateSubmenu answers an entry by its own key, goes back a menu on the back key, and hands huh
// every other message.
func (m model) updateSubmenu(msg tea.Msg, parents []submenuLevel, level submenuLevel) (tea.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateForm(msg)
	}

	if len(parents) > 0 && isBound(press.String(), formKeys().Back) {
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

	next, cmd := m.updateForm(msg)
	if !level.isOnDivider() {
		return next, cmd
	}

	moved, ok := next.(model)
	if !ok {
		return next, cmd
	}

	past, more := moved.updateForm(msg)

	return past, tea.Batch(cmd, more)
}

func (l submenuLevel) isOnDivider() bool {
	choice := *l.choice

	return choice >= 0 && choice < len(l.menu.entries) && l.menu.entries[choice].isDivider
}
