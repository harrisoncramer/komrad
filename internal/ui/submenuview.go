package ui

import (
	"slices"
	"strconv"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/zone"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// submenuLeading is the menus under the one on top rendered gray, as many as fit beside it and its preview.
func submenuLeading(parents []submenuLevel, top submenuLevel, width int) []string {
	room := width - lipgloss.Width(frozenSubmenu(top, len(parents) > 0))
	if top.menu.preview != nil {
		room -= columnFloor + dividerCols
	}

	return frozenMenus(parents, room)
}

// frozenMenus is a stack of menus rendered gray, as many of the nearest as fit in room.
func frozenMenus(levels []submenuLevel, room int) []string {
	var rendered []string

	for i := len(levels) - 1; i >= 0; i-- {
		view := frozenSubmenu(levels[i], i > 0)

		room -= lipgloss.Width(view) + dividerCols
		if room < 0 {
			break
		}

		rendered = append(rendered, markParentMenu(view, i))
	}

	slices.Reverse(rendered)

	return rendered
}

// frozenSubmenu renders a menu once another has opened to its right.
func frozenSubmenu(level submenuLevel, nested bool) string {
	form := submenuForm(level, nested, fixedKeys()).WithShowHelp(false)
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

	if entry.isOff && entry.offSaid != "" {
		return muted.Render(text+pad+key) + "\n" + muted.Italic(true).Render("  "+entry.offSaid)
	}

	if entry.isOff {
		return muted.Render(text + pad + key)
	}

	return text + pad + muted.Render(key)
}

func markEntry(label string, at int) string {
	return zone.MarkKey(zoneMenu, lipgloss.Width(label), 0, strconv.Itoa(at)) + label
}

func markParentMenu(view string, at int) string {
	mark := zone.MarkKey(zoneParentMenu, lipgloss.Width(view), 0, strconv.Itoa(at))

	return strings.Join(xslice.Map(strings.Split(view, "\n"), func(line string) string { return mark + line }), "\n")
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
