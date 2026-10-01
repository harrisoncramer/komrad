package ui

import (
	"strings"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
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
		isOff: menu.firstOn() == submenuCancel,
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
	return formState{
		kind:   level.menu.kind,
		crumbs: append(xslice.Map(parents, func(p submenuLevel) string { return p.menu.crumb }), level.menu.crumb),
		update: func(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
			return m.updateSubmenu(msg, parents, level)
		},
		submit: func(m model) (tea.Model, tea.Cmd) {
			if *level.choice == submenuCancel {
				return m.backSubmenu(parents)
			}

			entry := level.menu.entries[*level.choice]
			stack := append(parents[:len(parents):len(parents)], level)

			switch {
			case entry.opens != nil:
				return m.pushSubmenu(stack, newSubmenuLevel(*entry.opens))
			case entry.form != nil:
				return m.stackForm(stack, entry.form())
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
		leading: func(width, _ int) []string {
			return submenuLeading(parents, level, width)
		},
	}
}

// pushSubmenu draws a menu to the right of the one it was opened from.
func (m model) pushSubmenu(parents []submenuLevel, level submenuLevel) (tea.Model, tea.Cmd) {
	cmd := m.openForm(submenuState(parents, level))

	return m, cmd
}

// stackForm draws a form to the right of the menus it was opened from.
func (m model) stackForm(menus []submenuLevel, form formState) (tea.Model, tea.Cmd) {
	form.crumbs = append(xslice.Map(menus, func(l submenuLevel) string { return l.menu.crumb }), form.crumbs...)
	form.leading = func(width, _ int) []string {
		return frozenMenus(menus, width-formColumnFloor)
	}
	form.back = func(m model) (tea.Model, tea.Cmd) {
		return m.backSubmenu(menus)
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
	menu := level.menu

	labelCols, keyCols := submenuColumns(menu.entries)

	options := make([]huh.Option[int], 0, len(menu.entries)+1)
	for i, entry := range menu.entries {
		options = append(options, huh.NewOption(submenuLabel(entry, labelCols, keyCols), i))
	}

	last := "cancel"
	if nested {
		last = "back"
	}
	options = append(options, huh.NewOption(last, submenuCancel))

	field := huh.NewSelect[int]().
		Key("submenu").
		Title(menu.title).
		Value(level.choice).
		Options(options...)

	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(true).WithKeyMap(submenuKeyMap(keys)).WithTheme(paint.Panel.FormTheme())
}

// submenuLeading is the menus under the one on top drawn gray, as many of the nearest as leave the
// top its own width.
func submenuLeading(parents []submenuLevel, top submenuLevel, width int) []string {
	return frozenMenus(parents, width-lipgloss.Width(frozenSubmenu(top, len(parents) > 0)))
}

// frozenMenus is a stack of menus drawn gray, as many of the nearest as fit in room.
func frozenMenus(levels []submenuLevel, room int) []string {
	var drawn []string

	for i := len(levels) - 1; i >= 0; i-- {
		view := frozenSubmenu(levels[i], i > 0)

		room -= lipgloss.Width(view) + dividerCols
		if room < 0 {
			break
		}

		drawn = append([]string{view}, drawn...)
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
