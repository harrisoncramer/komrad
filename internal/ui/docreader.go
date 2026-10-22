package ui

import (
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/reader"
	"komrad/internal/ui/screen"

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

	spec := m.columnOf()
	if spec.read == nil || !spec.read(m) {
		return false
	}

	_, ok := m.screenColumnSplit()

	return ok
}

// isDocFocused reports whether the document beside the list holds the keys.
func (m *model) isDocFocused() bool {
	return m.reading.focused && m.isDocReadable() && m.isListFocused()
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

// placeDoc puts the reader on a line of the document with its window from top.
func (m *model) placeDoc(line, top int) {
	m.reading.tail = false
	if _, _, ok := m.docLayout(); ok {
		m.reading.Line, m.reading.Top = line, top
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
		return r.filter.settleCmd(m.clock, func(query string) tea.Msg { return docFilterSettledMsg{query: query} })
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

	line, found := reader.Jump(reader.Matches(r.filter.pattern, rows), r.Line, keys.DirectionOf(pressed, m.keys.Filter.PrevMatch))
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
	if _, ok := m.active().(recordRenderer); ok && keys.IsBound(k, fixedKeys().Quit) {
		return m.hide()
	}

	own := m.docActions()
	if slices.ContainsFunc(own.Table, func(a keys.Action[*model]) bool { return keys.IsBound(k, a.Keys(m.keys)) }) {
		return m.apply(own.Dispatch(m, m.keys, k))
	}

	if m.screen == screen.Documents {
		return m.apply(m.documents.docKeys().Dispatch(m, m.keys, k))
	}

	return nil
}

// docActions is the keys on the document beside the list once the picker has handed them to it.
func (m *model) docActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: []keys.Action[*model]{
			{
				ID:   keys.ColumnBack,
				Keys: keys.FixedBack,
				Nav: func(*model, string) screen.Request {
					m.reading.focused = false

					return nil
				},
			},
			{
				ID:   keys.Move,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				Nav: func(_ *model, pressed string) screen.Request {
					m.moveDocTo(m.reading.Line + keys.DirectionOf(pressed, m.keys.Nav.Up))

					return nil
				},
			},
			{
				ID:   keys.NavPage,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				Nav: func(_ *model, pressed string) screen.Request {
					m.moveDocTo(m.reading.Line + keys.DirectionOf(pressed, m.keys.Nav.PageUp)*navPageStep)

					return nil
				},
			},
			{
				ID:   keys.ColumnScroll,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				Nav: func(_ *model, pressed string) screen.Request {
					m.scrollDoc(keys.DirectionOf(pressed, m.keys.Column.ScrollUp) * columnWheelStep)

					return nil
				},
			},
			{
				ID:   keys.Jump,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
				Nav: func(_ *model, pressed string) screen.Request {
					line := 0
					if rows, _, ok := m.docLayout(); ok && keys.IsBound(pressed, m.keys.Nav.Bottom) {
						line = len(rows) - 1
					}
					m.moveDocTo(line)

					return nil
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav: func(*model, string) screen.Request {
					m.settleTail()
					m.reading.filter.open(m.columnOf().searchOptions...)
					m.reading.origin = m.reading.Line

					return nil
				},
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				Nav: func(_ *model, pressed string) screen.Request {
					m.jumpDocMatch(pressed)

					return nil
				},
			},
		},
		Legend: []keys.LegendRow[*model]{
			keys.MoveRow[*model](),
			keys.NavPageRow[*model](),
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: func(*model) string { return "search" },
			},
			{
				Of:   []keys.ID{keys.FilterMatch},
				Desc: func(*model) string { return "next/previous match" },
				Label: func(k config.Keybindings) string {
					return keys.HeadLabel(k.Filter.NextMatch) + "/" + keys.HeadLabel(k.Filter.PrevMatch)
				},
			},
			keys.JumpRow[*model](),
			{
				Of:    []keys.ID{keys.ColumnBack},
				Desc:  func(*model) string { return "back to the list" },
				Strip: true,
			},
		},
	}
}

// docHelp is the legend while the document beside the list holds the keys.
func (m *model) docHelp() keyMap {
	own := m.helpOf(m.docActions())
	if m.screen == screen.Documents {
		screen := m.helpOf(m.documents.docKeys())
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
