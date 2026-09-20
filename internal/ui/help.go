package ui

import (
	"slices"
	"strings"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// maxHelpColumns is the widest the expanded legend goes.
const maxHelpColumns = 3

type keyMap struct {
	short []key.Binding
	full  []key.Binding
}

func byLabel(bindings []key.Binding) []key.Binding {
	sorted := slices.Clone(bindings)
	slices.SortFunc(sorted, func(a, b key.Binding) int {
		return strings.Compare(a.Help().Desc, b.Help().Desc)
	})

	return sorted
}

func (m model) renderHelp(k keyMap, above ...string) string {
	if m.isReviewThreadHolding() {
		k = m.reviewThreadHelp()
	}
	if m.isDetailsFocused() {
		k = m.detailsHelp()
	}
	if m.isTabsFocused() {
		k = m.tabsHelp()
	}
	if m.isMessagesFocused() {
		k = m.messagesHelp()
	}
	if m.filter.typing {
		k = filterHelp(m.filter.mode)
	}

	width := m.paneWidth()
	view := helpView(width)

	if !m.helpExpanded {
		return fitLegend(view.ShortHelpView(byLabel(k.short)), width) + "\n"
	}

	// bubbles truncates a column wider than it was given to a single ellipsis rather than wrapping
	// it, and helpColumns has already dropped to one column by then, so a legend the sidebar
	// cannot hold gives way to its short form, a width of zero being a panel that has not been
	// told how wide it is and which bubbles reads as no limit.
	columns := helpColumns(byLabel(k.full), width)
	full := fitLegend(view.FullHelpView(columns), width) + "\n"
	if (width > 0 && helpWidth(columns) > width) || !m.doesLegendLeaveARow(full, above) {
		return fitLegend(view.ShortHelpView(m.refusedExpansion(byLabel(k.short))), width) + "\n"
	}

	return full
}

func (m model) doesLegendLeaveARow(legend string, above []string) bool {
	if m.height <= 0 || len(above) == 0 {
		return true
	}

	return m.spareLines(slices.Concat(above, []string{legend})...) >= 1
}

// refusedExpansion re-labels the legend's own key for the short form drawn in place of an
// expansion there was no room for.
func (m model) refusedExpansion(bindings []key.Binding) []key.Binding {
	out := slices.Clone(bindings)
	for i, b := range out {
		if slices.Equal(b.Keys(), m.keys.Help.Toggle) {
			out[i] = binding(m.keys.Help.Toggle, "show help")
		}
	}

	return out
}

func helpView(width int) help.Model {
	view := help.New()
	view.Styles = paint.HelpStyles()
	view.Width = width

	return view
}

// helpColumns lays the expanded legend out in as many columns as the sidebar can hold, dropping
// a column that does not fit rather than wrapping it.
func helpColumns(bindings []key.Binding, width int) [][]key.Binding {
	for columns := maxHelpColumns; columns > 1; columns-- {
		split := splitColumns(bindings, columns)
		if helpWidth(split) <= width {
			return split
		}
	}

	return [][]key.Binding{bindings}
}

// helpWidth is what the legend wants, measured by the same code that draws it at a width of
// zero, which truncates nothing.
func helpWidth(columns [][]key.Binding) int {
	return lipgloss.Width(helpView(0).FullHelpView(columns))
}

func splitColumns(bindings []key.Binding, columns int) [][]key.Binding {
	rows := (len(bindings) + columns - 1) / columns

	split := make([][]key.Binding, 0, columns)
	for start := 0; start < len(bindings); start += rows {
		split = append(split, bindings[start:min(start+rows, len(bindings))])
	}

	return split
}

// fitLegend cuts each row of the legend to the column it is drawn in, since bubbles keeps adding
// items once the ellipsis it would put in their place no longer fits.
func fitLegend(rendered string, width int) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " ")
		if width > 0 {
			line = ansi.Truncate(line, width, "…")
		}
		lines[i] = line
	}

	return strings.Join(lines, "\n")
}

func binding(keys []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keyLabel(keys), desc))
}

func headLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}

	return keyLabel(keys[:1])
}

func (m model) moveBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Nav.Up)+"/"+keyLabel(m.keys.Nav.Down), "move"),
	)
}

func (m model) jumpBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Top, m.keys.Nav.Bottom)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(doubledLabel(m.keys.Nav.Top)+"/"+keyLabel(m.keys.Nav.Bottom), "top/bottom"),
	)
}

func (m model) navPageBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.PageUp, m.keys.Nav.PageDown)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(navPageLabel(m.keys), "page"),
	)
}

func doubledLabel(keys []string) string {
	pairs := make([]string, len(keys))
	for i, k := range keys {
		label := keyLabel([]string{k})
		pairs[i] = label + label
	}

	return strings.Join(pairs, "/")
}

func filterHelp(mode filterMode) keyMap {
	done := binding([]string{"enter"}, "keep filter")
	flip := binding([]string{"tab"}, mode.flipped().String()+" instead")
	drop := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{done, flip, drop}, full: []key.Binding{done, flip, drop}}
}

// infoPaneHelp is the keys that fill the pane beside the panel, drawn off the same question the
// dispatch asks before answering them.
func (m model) infoPaneHelp() []key.Binding {
	if !m.screen.opensInfoPane() {
		return nil
	}

	return slices.Concat(
		[]key.Binding{binding(m.keys.Logs.Open, m.logsDesc())},
		m.olderLogBindings(),
		[]key.Binding{binding(m.keys.Terminal.Open, m.terminalDesc())},
	)
}

// olderLogBindings appear only while the info pane is showing a run, naming which of an agent's
// runs is on screen.
func (m model) olderLogBindings() []key.Binding {
	if m.info.kind != infoRunLog {
		return nil
	}

	return []key.Binding{binding(m.keys.Logs.Older, m.runPosition())}
}

func (m model) helpBinding() key.Binding {
	desc := "show help"
	if m.helpExpanded {
		desc = "hide help"
	}

	return binding(m.keys.Help.Toggle, desc)
}

func (m model) globalHelp() []key.Binding {
	return []key.Binding{m.helpBinding()}
}

func (m model) agentHelp() keyMap {
	own := agentsActions().help(m)

	// The keys every screen shares are appended rather than written into each screen's table.
	full := slices.Concat(
		own.full,
		[]key.Binding{
			m.tabsBinding(),
			m.detailsBinding(),
			m.messagesBinding(),
		},
		m.infoPaneHelp(),
		[]key.Binding{m.columnScrollBinding("the notes")},
		m.globalHelp(),
	)

	return keyMap{short: slices.Concat(own.short, m.globalHelp()), full: full}
}

func (m model) detailsBinding() key.Binding {
	desc := "details"
	if m.isDetailsFocused() {
		desc = "back to the list"
	}

	return binding(m.keys.Details.Focus, desc)
}

func (m model) detailsHelp() keyMap {
	copyField := binding(m.keys.Details.Copy, "copy field")
	back := binding(m.keys.Details.Focus, "back to the list")

	return keyMap{
		short: []key.Binding{copyField, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{copyField, back, m.moveBinding(), m.jumpBinding()}, m.globalHelp()),
	}
}

func (m model) showHiddenDesc() string {
	if m.tree.showHidden {
		return "put hidden away"
	}

	return "show hidden"
}

func (m model) scheduleHelp() keyMap {
	own := schedulesActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			[]key.Binding{
				m.detailsBinding(),
				m.messagesBinding(),
			},
			m.infoPaneHelp(),
			[]key.Binding{m.columnScrollBinding("the record")},
			m.globalHelp(),
		),
	}
}

func (m model) archiveHelp() keyMap {
	own := archiveActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			[]key.Binding{
				m.tabsBinding(),
				m.detailsBinding(),
				m.messagesBinding(),
				m.columnScrollBinding("the notes"),
			},
			m.globalHelp(),
		),
	}
}

func (m model) worktreesHelp() keyMap {
	own := worktreesActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			[]key.Binding{
				m.tabsBinding(),
				m.detailsBinding(),
				m.messagesBinding(),
				m.columnScrollBinding("the directory"),
			},
			m.globalHelp(),
		),
	}
}

// columnScrollBinding is the pair that moves the block beside the list, drawn as one row since
// they are the same key going either way.
func (m model) columnScrollBinding(what string) key.Binding {
	return key.NewBinding(
		key.WithKeys(slices.Concat(m.keys.Column.ScrollDown, m.keys.Column.ScrollUp)...),
		key.WithHelp(
			keyLabel(m.keys.Column.ScrollDown)+"/"+keyLabel(m.keys.Column.ScrollUp),
			"scroll "+what,
		),
	)
}

func (m model) searchHelp() keyMap {
	if m.search.typing {
		done := binding([]string{"enter"}, "keep it, then move through the hits")
		leave := binding([]string{"esc"}, "back")

		return keyMap{short: []key.Binding{done, leave}, full: []key.Binding{done, leave}}
	}

	open := binding([]string{"enter"}, "go to it")
	edit := binding(slices.Concat([]string{"esc"}, m.keys.Filter.Open), "edit the query")

	return keyMap{
		short: []key.Binding{open, edit, m.helpBinding()},
		full: slices.Concat(
			[]key.Binding{
				open,
				edit,
				m.moveBinding(),
				m.jumpBinding(),
				m.navPageBinding(),
				m.columnScrollBinding("the transcript"),
			},
			m.globalHelp(),
		),
	}
}

// reviewHelp keeps the strip to what the screen is for.
func (m model) reviewHelp() keyMap {
	own := reviewActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			[]key.Binding{m.detailsBinding()},
			m.globalHelp(),
		),
	}
}

// isReviewThreadHolding reports whether the reader has taken the keys onto the conversation.
func (m model) isReviewThreadHolding() bool {
	if m.screen != screenReview || m.review.composing || m.review.releasing {
		return false
	}

	_, ok := m.reviewThreadAtCursor()

	return ok && m.review.threadFocus
}

// reviewThreadHelp is the legend while the conversation holds the keys.
func (m model) reviewThreadHelp() keyMap {
	scroll := binding(concatKeys(m.keys.Nav.Up, m.keys.Nav.Down), "scroll the thread")
	ends := binding(concatKeys(m.keys.Nav.Top, m.keys.Nav.Bottom), "top/bottom")
	back := binding(m.keys.Review.FocusThread, "back to the diff")
	reply := binding(m.keys.Review.Compose, "reply")
	settle := binding(m.keys.Review.Resolve, m.reviewResolveDesc())
	send := binding(formKeys().Submit, "send the review")

	return keyMap{
		short: slices.Concat([]key.Binding{scroll, back, reply, settle}, m.globalHelp()),
		full:  slices.Concat([]key.Binding{scroll, ends, back, reply, settle, send}, m.globalHelp()),
	}
}

// reviewThreadDesc names the key that moves between the diff and the conversation beside it,
// read from the flag and not from whether the thread holds the keys.
func (m model) reviewThreadDesc() string {
	if _, ok := m.reviewThreadAtCursor(); ok && m.review.threadFocus {
		return "back to the diff"
	}

	return "read the thread"
}

func keyLabel(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		if k == " " {
			out[i] = "space"
			continue
		}

		out[i] = strings.Replace(k, "ctrl+", "C-", 1)
	}

	return strings.Join(out, "/")
}
