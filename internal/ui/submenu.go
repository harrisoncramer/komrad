package ui

import (
	"strings"

	"komrad/config"
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
const nestedMark = " ›"

// submenu is a short list drawn beside the list it was opened over, each entry picked by its
// own key as well as by the cursor.
type submenu struct {
	kind    formKind
	title   string
	crumb   string
	entries []submenuEntry
	// answer runs the entry picked, by its index, and is never handed the cancel entry or an entry
	// that opens a menu.
	answer func(m model, choice int) (tea.Model, tea.Cmd)
}

// submenuEntry is one row of a submenu.
type submenuEntry struct {
	label string
	keys  []string
	// isOff says the entry would do nothing here.
	isOff bool
	// opens is the menu the entry opens to the right of its own.
	opens *submenu
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

			if child := level.menu.entries[*level.choice].opens; child != nil {
				return m.pushSubmenu(append(parents[:len(parents):len(parents)], level), newSubmenuLevel(*child))
			}

			return level.menu.answer(m, *level.choice)
		},
		rebuild: func(model) (*huh.Form, []string) {
			return submenuForm(level, len(parents) > 0, formKeys()), nil
		},
		leading: func(width int) []string {
			return submenuLeading(parents, level, width)
		},
	}
}

// pushSubmenu draws a menu to the right of the one it was opened from.
func (m model) pushSubmenu(parents []submenuLevel, level submenuLevel) (tea.Model, tea.Cmd) {
	cmd := m.openForm(submenuState(parents, level))

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

	options := make([]huh.Option[int], 0, len(menu.entries)+1)
	for i, entry := range menu.entries {
		options = append(options, huh.NewOption(submenuLabel(entry), i))
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
	room := width - lipgloss.Width(frozenSubmenu(top, len(parents) > 0))

	var drawn []string

	for i := len(parents) - 1; i >= 0; i-- {
		view := frozenSubmenu(parents[i], i > 0)

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

// submenuLabel is an entry's label with the key that picks it underlined, and gray when it
// would do nothing.
func submenuLabel(entry submenuEntry) string {
	label := entry.label

	if before, mark, after, ok := splitMark(label, headLabel(entry.keys)); ok {
		label = before + ansi.Style{}.Underline(true).String() + mark + ansi.Style{}.Underline(false).String() + after
	}

	if entry.opens != nil {
		label += nestedMark
	}

	if entry.isOff {
		return paint.Panel.Style(paint.ToneMuted).Render(label)
	}

	return label
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

	return m.updateForm(msg)
}
