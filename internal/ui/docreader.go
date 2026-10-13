package ui

import (
	"slices"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"

	tea "charm.land/bubbletea/v2"
)

// docReader is where the reader stands in the document rendered beside the list, and what they
// searched it for.
type docReader struct {
	reader.Reader
	// filter is the pattern the user searches the document for.
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

// isDocReadable reports whether the block beside the list is a document the panel walks itself.
func (m *model) isDocReadable() bool {
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
func (m *model) isDocFocused() bool {
	return m.reading.focused && m.isDocReadable() && m.isListFocused()
}

// docHead is the rows over the document while it holds the keys or a search: the search and what
// the last key said.
func (m *model) docHead(rows []string, width int) []string {
	r := m.reading
	if !m.isDocFocused() && !r.filter.typing && !r.filter.isStanding() {
		return nil
	}

	count := ""
	if r.filter.isSearched() {
		count = dimStyle().Render("  " + reader.Count(reader.Matches(r.filter.pattern, rows), r.Line))
	}

	return r.filter.head(m, count, r.said, width)
}

// docBlock is the document at the width of the block beside the list, under the search over it,
// from where the reader has moved to.
func (m *model) docBlock(split columnSplit) []string {
	rows := m.columnRows(split)
	head := m.docHead(rows, split.column)
	height := max(split.rows-len(head), 1)

	r := m.reading
	focused := m.isDocFocused()
	rendered := make([]string, len(rows))
	for i, row := range rows {
		rendered[i] = paint.Panel.HighlightStyled(row, reader.Spans(r.filter.pattern, row))
		if focused && i == r.Line {
			rendered[i] = paint.Panel.PaintRow(rendered[i], split.column)
		}
		rendered[i] = m.markRow(zoneDoc, split.column, strconv.Itoa(i), rendered[i])
	}

	window := docWindow{
		rows:    rendered,
		top:     r.Top,
		markers: true,
	}
	if r.tail {
		window.top = len(rendered)
	}

	return append(head, strings.Split(window.view(height), "\n")...)
}

// docLayout is the document rendered for the block beside the list, and the rows of the window
// under the search.
func (m *model) docLayout() ([]string, int, bool) {
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

// focusDoc hands the keys to the document, keeping the cursor on a row the window renders.
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
// keys back from a document the block no longer renders.
func (m *model) dropStaleDoc() {
	if shown := m.docShown(); m.reading.shown != shown {
		m.reading = docReader{shown: shown, tail: m.isScreenShown()}
	}

	if m.reading.focused && !m.isDocReadable() {
		m.reading.focused = false
	}
}

// updateDocFilter is the search prompt over the document taking keys, moving to the first match
// from where it opened as the pattern grows.
func (m *model) updateDocFilter(key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "ctrl+c" {
		return m.hide()
	}

	r := &m.reading
	if !r.filter.takeKey(key, m.register) {
		return nil
	}

	if !r.filter.isSettled() {
		return r.filter.settleCmd(func(query string) tea.Msg { return docFilterSettledMsg{query: query} })
	}

	m.findDocMatch()

	return nil
}

type docFilterSettledMsg struct {
	query string
}

func (m *model) applyDocFilterSettled(msg docFilterSettledMsg) {
	if m.reading.filter.settle(msg.query) {
		m.findDocMatch()
	}
}

func (m *model) findDocMatch() {
	r := &m.reading
	if !r.filter.isSearched() {
		m.moveDocTo(r.origin)

		return
	}

	rows, _, ok := m.docLayout()
	if !ok {
		return
	}

	if line, found := reader.From(reader.Matches(r.filter.pattern, rows), r.origin); found {
		m.moveDocTo(line)
	}
}

// jumpDocMatch steps to the next or previous row the search found, wrapping at the ends.
func (m *model) jumpDocMatch(pressed string) {
	r := &m.reading
	rows, _, ok := m.docLayout()
	if !ok {
		return
	}

	line, found := reader.Jump(reader.Matches(r.filter.pattern, rows), r.Line, directionOf(pressed, m.keys.Filter.PrevMatch))
	if !found {
		r.said = "nothing to search for yet"
		if r.filter.isSearched() {
			r.said = "no matches"
		}
		m.moveDocTo(r.Line)

		return
	}

	m.moveDocTo(line)
}

// updateDoc is the document taking the keys, handing what it does not answer to the screen's own
// keys on it.
func (m *model) updateDoc(key tea.KeyPressMsg) tea.Cmd {
	m.reading.said = ""

	k := key.String()
	if _, ok := m.active().(recordRenderer); ok && isBound(k, fixedKeys().Quit) {
		return m.hide()
	}

	own := m.docActions()
	if slices.ContainsFunc(own.table, func(a screenAction) bool { return isBound(k, a.keys(m.keys)) }) {
		return own.dispatch(m, k)
	}

	if m.screen == screenDocuments {
		return m.documents.docKeys().dispatch(m, k)
	}

	return nil
}

// docActions is the keys on the document beside the list once the picker has handed them to it.
func (m *model) docActions() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionColumnBack,
				keys: fixedBack,
				nav: func(*model, string) tea.Cmd {
					m.reading.focused = false

					return nil
				},
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.moveDocTo(m.reading.Line + directionOf(pressed, m.keys.Nav.Up))

					return nil
				},
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.moveDocTo(m.reading.Line + directionOf(pressed, m.keys.Nav.PageUp)*navPageStep)

					return nil
				},
			},
			{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.scrollDoc(directionOf(pressed, m.keys.Column.ScrollUp) * columnWheelStep)

					return nil
				},
			},
			{
				id:   actionJump,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
				nav: func(_ *model, pressed string) tea.Cmd {
					line := 0
					if rows, _, ok := m.docLayout(); ok && isBound(pressed, m.keys.Nav.Bottom) {
						line = len(rows) - 1
					}
					m.moveDocTo(line)

					return nil
				},
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				nav: func(*model, string) tea.Cmd {
					m.settleTail()
					spec, _ := m.columnOf()
					m.reading.filter.open(spec.searchOptions...)
					m.reading.origin = m.reading.Line

					return nil
				},
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.jumpDocMatch(pressed)

					return nil
				},
			},
		},
		legend: []screenLegendRow{
			{
				of:    []actionID{actionMove},
				desc:  func(*model) string { return "move" },
				label: moveLabel,
				strip: true,
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(*model) string { return "page" },
				label: navPageLabel,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: func(*model) string { return "search" },
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(*model) string { return "next/previous match" },
				label: func(k config.Keybindings) string {
					return headLabel(k.Filter.NextMatch) + "/" + headLabel(k.Filter.PrevMatch)
				},
			},
			{
				of:    []actionID{actionJump},
				desc:  func(*model) string { return "top/bottom" },
				label: jumpLabel,
			},
			{
				of:    []actionID{actionColumnBack},
				desc:  func(*model) string { return "back to the list" },
				strip: true,
			},
		},
	}
}

// docHelp is the legend while the document beside the list holds the keys.
func (m *model) docHelp() keyMap {
	own := m.docActions().help(m)
	if m.screen == screenDocuments {
		screen := m.documents.docKeys().help(m)
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
func isAlwaysRead(*model) bool {
	return true
}

// wheelDoc moves the window over the document, carrying the cursor along when it leaves the window.
func (m *model) wheelDoc(delta int) {
	m.settleTail()

	if rows, height, ok := m.docLayout(); ok {
		m.reading.Scroll(delta, len(rows), height)
		m.reading.Settle(len(rows), height)
	}
}

func (m *model) clickDocLine(key string) tea.Cmd {
	line, err := strconv.Atoi(key)
	if err != nil {
		return nil
	}

	m.moveDocTo(line)

	return nil
}
