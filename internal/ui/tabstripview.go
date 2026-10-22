package ui

func (m *model) tabbedHeader(title, tally string) string {
	var strips string
	if m.isTabsRendered() {
		strips += m.tabsLine()
	}
	if m.isKindsRendered() {
		strips += m.kindsLine()
	}
	if strips == "" {
		return m.talliedHeader(title, tally)
	}

	return m.headerLines(title, tally, strips)
}

func (m *model) tabsLine() string {
	t, _ := m.activeTabs()

	strip := t.Tabs()

	return strip.View(*t.TabsState(), m.paneWidth(), m.spanMarker(zoneTabs, strip.StopKeys())) + "\n"
}
