package menu

import (
	"slices"

	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// Cancel is the choice of the cancel entry every menu ends on.
const Cancel = -1

// divider is a rule between two bands of entries.
var divider = Entry{Off: true, IsDivider: true}

// Menu is a short list beside the list it opened over, with the cursor and each entry's own key
// picking an entry.
type Menu struct {
	Title   string
	Entries []Entry
	// ActionsForChoice is the menu the menu key opens on the entry under a list's cursor, nil for
	// a menu whose entries act on their own.
	ActionsForChoice func(choice int) Menu
	// Preview renders the entry under a list's cursor beside the list, within width and height.
	Preview func(choice, width, height int) string
	// EmptyListRow is the row a list renders while it holds nothing.
	EmptyListRow string
	// FormReplacesMenu says a form an entry opens takes the menu's place rather than opening
	// beside it.
	FormReplacesMenu bool
}

// after is what becomes of the menu once an entry is picked.
type after int

const (
	// CloseBeforePick takes the menu down before the pick runs.
	CloseBeforePick after = iota
	// Hold keeps the menu up while a write the pick began is in flight.
	Hold
)

// Entry is one row of a menu.
type Entry struct {
	Label string
	Keys  []string
	// Off says the entry would do nothing here.
	Off bool
	// OffSaid is why the entry would do nothing here, drawn under it.
	OffSaid string
	// IsDivider says the row is a rule between two bands of entries.
	IsDivider bool
	// Submenu is the menu the entry opens to the right of its own.
	Submenu *Menu
	// Form is the form the entry opens beside the menu.
	Form      func() form.Form
	Pick      func(screen.Inputs) screen.Request
	AfterPick after
}

// NestedMenuBuilder is what builds the git and jump menus a band nests.
type NestedMenuBuilder interface {
	Git(task panel.Task) Menu
	PullRequestGit(task panel.Task) Menu
	Jump(fromScreen screen.ID, target screen.Target) Menu
}

// Open renders a menu, its cursor on the first entry that would do something.
type Open struct {
	Menu Menu
}

// Request marks Open as a screen request.
func (Open) Request() {}

// IsList reports whether the menu is a list whose entries open a menu of their own.
func (m Menu) IsList() bool {
	return m.ActionsForChoice != nil
}

// FirstOn is the first entry that would do something, Cancel when none would.
func (m Menu) FirstOn() int {
	for i, entry := range m.Entries {
		if !entry.Off {
			return i
		}
	}

	return Cancel
}

// Nested is an entry that opens a menu, off when nothing inside it would do anything.
func Nested(label string, keys []string, menu Menu) Entry {
	return Entry{
		Label:   label,
		Keys:    keys,
		Off:     !menu.IsList() && menu.FirstOn() == Cancel,
		Submenu: &menu,
	}
}

// WithNested is a band with a nested menu at its foot.
func WithNested(menu Menu, label string, keys []string, nested Menu) Menu {
	at := len(menu.Entries)
	menu.Entries = append(menu.Entries[:at:at], Nested(label, keys, nested))

	return menu
}

// Join is one band over another with a rule between them, under the top band's title.
func Join(top, bottom Menu) Menu {
	return Menu{
		Title:   top.Title,
		Entries: slices.Concat(top.Entries, []Entry{divider}, bottom.Entries),
	}
}

// DrawnOff is an entry drawn off with the reason it does nothing here, or the entry itself when
// said is empty.
func DrawnOff(entry Entry, said string) Entry {
	if said == "" {
		return entry
	}

	entry.Off, entry.OffSaid = true, said

	return entry
}

// Leaving is an entry that takes the menu down before pick runs.
func Leaving(label string, keys []string, pick func(screen.Inputs) screen.Request) Entry {
	return Entry{Label: label, Keys: keys, Pick: pick}
}

// Holding is an entry that keeps the menu up while a write pick began is in flight.
func Holding(label string, keys []string, pick func(screen.Inputs) screen.Request) Entry {
	return Entry{Label: label, Keys: keys, Pick: pick, AfterPick: Hold}
}
