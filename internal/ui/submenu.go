package ui

import (
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// submenuCancel is the choice of the cancel entry every submenu ends on.
const submenuCancel = -1

// submenu is a short list drawn beside the list it was opened over, each entry picked by its
// own key as well as by the cursor.
type submenu struct {
	kind    formKind
	title   string
	crumb   string
	entries []submenuEntry
	// answer runs the entry picked, by its index, and is never handed the cancel entry.
	answer func(m model, choice int) (tea.Model, tea.Cmd)
}

// submenuEntry is one row of a submenu.
type submenuEntry struct {
	label string
	keys  []string
	// isOff says the entry would do nothing here, which draws it gray and leaves its answer to
	// say why.
	isOff bool
}

// openSubmenu draws a submenu, its cursor on the first entry that would do something.
func openSubmenu(h host, menu submenu) tea.Cmd {
	choice := menu.firstOn()

	return h.openForm(formState{
		kind:  menu.kind,
		crumb: menu.crumb,
		update: func(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
			return m.updateSubmenu(msg, menu, &choice)
		},
		submit: func(m model) (tea.Model, tea.Cmd) {
			if choice == submenuCancel {
				return m.closeForm()
			}

			return menu.answer(m, choice)
		},
		rebuild: func(model) (*huh.Form, []string) {
			return submenuForm(menu, &choice, formKeys()), nil
		},
	})
}

func (s submenu) firstOn() int {
	for i, entry := range s.entries {
		if !entry.isOff {
			return i
		}
	}

	return submenuCancel
}

func submenuForm(menu submenu, choice *int, keys config.FormKeys) *huh.Form {
	options := make([]huh.Option[int], 0, len(menu.entries)+1)
	for i, entry := range menu.entries {
		options = append(options, huh.NewOption(submenuLabel(entry), i))
	}
	options = append(options, huh.NewOption("cancel", submenuCancel))

	field := huh.NewSelect[int]().
		Key("submenu").
		Title(menu.title).
		Value(choice).
		Options(options...)

	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(true).WithKeyMap(submenuKeyMap(keys)).WithTheme(paint.Panel.FormTheme())
}

// submenuLabel is an entry's label with the key that picks it underlined, and gray when it
// would do nothing.
func submenuLabel(entry submenuEntry) string {
	label := entry.label

	if mark := headLabel(entry.keys); mark != "" {
		if at := strings.Index(label, mark); at >= 0 {
			end := at + len(mark)
			label = label[:at] + ansi.Style{}.Underline(true).String() + label[at:end] +
				ansi.Style{}.Underline(false).String() + label[end:]
		}
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

// updateSubmenu answers an entry by its own key, and hands huh every other message.
func (m model) updateSubmenu(msg tea.Msg, menu submenu, choice *int) (tea.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateForm(msg)
	}

	for i, entry := range menu.entries {
		if isBound(press.String(), entry.keys) {
			*choice = i

			return m.answerForm()
		}
	}

	return m.updateForm(msg)
}
