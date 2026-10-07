package ui

// frameBuilder is a screen that builds what its frame draws once an update has settled.
type frameBuilder interface {
	buildFrame(m *model)
}

// dropFrames forgets what the screens built for the last frame, so an update reads the state it changes.
func (m *model) dropFrames() {
	m.agents.drawn = nil
	m.worktrees.drawn = nil
	m.schedules.drawn = nil
	m.agents.archived.drawn = nil
	m.schedules.archived.drawn = nil
}

// buildFrame builds what the active screen draws from the state the update left.
func (m *model) buildFrame() {
	if builder, ok := m.active().(frameBuilder); ok {
		builder.buildFrame(m)
	}
}

// dropRenderedView forgets the frame View last rendered, keeping it while the panel waits to be told its size.
func (m *model) dropRenderedView() {
	if m.isAwaitingSize() {
		return
	}

	m.term.renderedView = ""
}
