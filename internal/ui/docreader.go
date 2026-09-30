package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"

	tea "charm.land/bubbletea/v2"
)

// docReader is where the reader stands in the document drawn beside the list, and what they
// searched it for.
type docReader struct {
	reader.Reader
	// filter is the pattern being searched for in the document.
	filter filterState
	// origin is the row the cursor was on when the search opened.
	origin int
	// said is what the last key on the document said.
	said string
	// focused says the picker handed the keys to the document.
	focused bool
	// shown names the row the document was read for.
	shown string
	// tail says the window stays on the last row until the reader moves it.
	tail bool
}

// docKeyed is a screen with keys of its own that answer while its document holds the keys.
type docKeyed interface {
	docKeys() screenActionSpec
}

// isDocReadable reports whether the block beside the list is a document the panel walks itself.
func (m model) isDocReadable() bool {
	if m.isMessagesFocused() {
		return false
	}

	spec, ok := m.columnOf()
	if !ok || spec.read == nil || !spec.read(m) {
		return false
	}

	_, ok = m.screenColumnSplit()

	return ok
}

// isDocFocused reports whether the document beside the list holds the keys.
func (m model) isDocFocused() bool {
	return m.reading.focused && m.isDocReadable() && m.isListFocused()
}

// docHead is the rows over the document while it holds the keys or a search: the search and what
// the last key said.
func (m model) docHead(rows []string, width int) []string {
	r := m.reading
	if !m.isDocFocused() && !r.filter.typing && !r.filter.isStanding() {
		return nil
	}

	count := ""
	if r.filter.isStanding() {
		count = dimStyle().Render("  " + reader.Count(reader.Matches(r.filter.pattern, rows), r.Line))
	}

	return r.filter.head(count, r.said, width, m.glyphs)
}

// docBlock is the document at the width of the block beside the list, under the search over it,
// from where the reader has moved to.
func (m model) docBlock(split columnSplit) []string {
	rows := m.columnRows(split)
	head := m.docHead(rows, split.column)
	height := max(split.rows-len(head), 1)

	r := m.reading
	focused := m.isDocFocused()
	drawn := make([]string, len(rows))
	for i, row := range rows {
		drawn[i] = paint.Panel.HighlightStyled(row, reader.Spans(r.filter.pattern, row))
		if focused && i == r.Line {
			drawn[i] = paint.Panel.PaintRow(drawn[i], split.column)
		}
	}

	window := docWindow{
		rows:    drawn,
		top:     r.Top,
		markers: true,
	}
	if r.tail {
		window.top = len(drawn)
	}

	return append(head, strings.Split(window.view(height), "\n")...)
}

// docLayout is the document rendered for the block beside the list, and the rows of the window
// under the search.
func (m model) docLayout() ([]string, int, bool) {
	split, ok := m.screenColumnSplit()
	if !ok || !m.isDocReadable() {
		return nil, 0, false
	}

	rows := m.columnRows(split)

	return rows, max(split.rows-len(m.docHead(rows, split.column)), 1), true
}

// moveDocTo puts the cursor on a row of the document and brings the window round to it.
func (m *model) moveDocTo(line int) {
	m.settleTail()
	if rows, height, ok := m.docLayout(); ok {
		m.reading.MoveTo(line, len(rows), height)
	}
}

// settleTail puts the reader on the last row of a document held at its end.
func (m *model) settleTail() {
	if !m.reading.tail {
		return
	}

	m.reading.tail = false
	if rows, height, ok := m.docLayout(); ok {
		m.reading.MoveTo(len(rows)-1, len(rows), height)
	}
}

// focusDoc hands the keys to the document, keeping the cursor on a row the window draws.
func (m *model) focusDoc() {
	rows, height, ok := m.docLayout()
	if !ok {
		return
	}

	m.settleTail()
	m.reading.focused = true
	m.reading.Settle(len(rows), height)
}

// scrollDoc moves the cursor through the document while it holds the keys, and the window over
// it otherwise.
func (m *model) scrollDoc(delta int) {
	m.settleTail()

	if m.isDocFocused() {
		m.moveDocTo(m.reading.Line + delta)

		return
	}

	if rows, height, ok := m.docLayout(); ok {
		m.reading.Scroll(delta, len(rows), height)
	}
}

// dropStaleDoc forgets where the reader stood once the block shows another row, and hands the
// keys back from a document the block no longer draws.
func (m model) dropStaleDoc() model {
	if shown := m.docShown(); m.reading.shown != shown {
		m.reading = docReader{shown: shown, tail: m.isConversationPreviewed()}
	}

	if m.reading.focused && !m.isDocReadable() {
		m.reading.focused = false
	}

	return m
}

// updateDocFilter is the search prompt over the document taking keys, moving to the first match
// from where it opened as the pattern grows.
func (m model) updateDocFilter(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	r := &m.reading
	if !r.filter.takeKey(key) {
		return m, nil
	}

	if !r.filter.isStanding() {
		m.moveDocTo(r.origin)

		return m, nil
	}

	rows, _, ok := m.docLayout()
	if !ok {
		return m, nil
	}

	if line, found := reader.From(reader.Matches(r.filter.pattern, rows), r.origin); found {
		m.moveDocTo(line)
	}

	return m, nil
}

// jumpDocMatch steps to the next or previous row the search found, wrapping at the ends.
func (m model) jumpDocMatch(pressed string) (tea.Model, tea.Cmd) {
	r := &m.reading
	rows, _, ok := m.docLayout()
	if !ok {
		return m, nil
	}

	line, found := reader.Jump(reader.Matches(r.filter.pattern, rows), r.Line, directionOf(pressed, m.keys.Filter.PrevMatch))
	if !found {
		r.said = "nothing to search for yet"
		if r.filter.isStanding() {
			r.said = "no matches"
		}
		m.moveDocTo(r.Line)

		return m, nil
	}

	m.moveDocTo(line)

	return m, nil
}

// updateDoc is the document taking the keys, handing what it does not answer to the screen's own
// keys on it.
func (m model) updateDoc(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.reading.said = ""

	k := key.String()
	if _, ok := m.active().(detailed); ok && isBound(k, m.quitKeys()) {
		return m, m.hide()
	}

	if slices.ContainsFunc(docActions().table, func(a action) bool { return isBound(k, a.keys(m.keys)) }) {
		return docActions().dispatch(m, k)
	}

	if keyed, ok := m.active().(docKeyed); ok {
		cmd := keyed.docKeys().dispatch(&m, m.shared(), k)

		return m, cmd
	}

	return m, nil
}

// docActions is the keys on the document beside the list once the picker has handed them to it.
func docActions() actionSpec {
	return actionSpec{
		table: []action{
			{
				id:   actionColumnBack,
				keys: func(config.Keybindings) []string { return []string{"esc"} },
				run: func(m model, _ string) (tea.Model, tea.Cmd) {
					m.reading.focused = false

					return m, nil
				},
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				run: func(m model, pressed string) (tea.Model, tea.Cmd) {
					m.moveDocTo(m.reading.Line + directionOf(pressed, m.keys.Nav.Up))

					return m, nil
				},
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				run: func(m model, pressed string) (tea.Model, tea.Cmd) {
					m.moveDocTo(m.reading.Line + directionOf(pressed, m.keys.Nav.PageUp)*navPageStep)

					return m, nil
				},
			},
			{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				run: func(m model, pressed string) (tea.Model, tea.Cmd) {
					m.scrollDoc(directionOf(pressed, m.keys.Column.ScrollUp) * columnWheelStep)

					return m, nil
				},
			},
			{
				id:   actionJump,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
				run: func(m model, pressed string) (tea.Model, tea.Cmd) {
					line := 0
					if rows, _, ok := m.docLayout(); ok && isBound(pressed, m.keys.Nav.Bottom) {
						line = len(rows) - 1
					}
					m.moveDocTo(line)

					return m, nil
				},
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run: func(m model, _ string) (tea.Model, tea.Cmd) {
					m.settleTail()
					m.reading.filter.typing = true
					m.reading.filter.setQuery("")
					m.reading.origin = m.reading.Line

					return m, nil
				},
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				run:  model.jumpDocMatch,
			},
		},
		legend: []legendRow{
			{
				of:    []actionID{actionMove},
				desc:  func(model) string { return "move" },
				label: moveLabel,
				strip: true,
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(model) string { return "page" },
				label: navPageLabel,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: func(model) string { return "search" },
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(model) string { return "next/previous match" },
				label: func(k config.Keybindings) string {
					return headLabel(k.Filter.NextMatch) + "/" + headLabel(k.Filter.PrevMatch)
				},
			},
			{
				of:    []actionID{actionJump},
				desc:  func(model) string { return "top/bottom" },
				label: jumpLabel,
			},
			{
				of:    []actionID{actionColumnBack},
				desc:  func(model) string { return "back to the list" },
				strip: true,
			},
		},
	}
}

// docHelp is the legend while the document beside the list holds the keys.
func (m model) docHelp() keyMap {
	own := docActions().help(m)
	if keyed, ok := m.active().(docKeyed); ok {
		screen := keyed.docKeys().help(m.shared())
		own = keyMap{
			short: slices.Concat(own.short, screen.short),
			full:  slices.Concat(own.full, screen.full),
		}
	}

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full:  slices.Concat(own.full, m.fullGlobalHelp()),
	}
}

// isAlwaysRead is the read of a block that is a document whatever the screen is showing.
func isAlwaysRead(model) bool {
	return true
}
