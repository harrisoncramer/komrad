package ui

func (m *model) tabbedHeader(title, tally string) string {
	in := m.inputs()
	var strips string
	if m.isTabsRendered(in) {
		strips += m.tabsLine()
	}
	if m.isKindsRendered(in) {
		strips += m.kindsLine()
	}
	if strips == "" {
		return m.talliedHeader(title, tally)
	}

	return m.headerLines(title, tally, strips)
}

func (m *model) tabsLine() string {
	t, _ := m.activeTabs()

	strip := t.tabs()

	return strip.View(*t.tabsState(), m.paneWidth(), m.spanMarker(zoneTabs, strip.StopKeys())) + "\n"
}
