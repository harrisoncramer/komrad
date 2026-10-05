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
	m.archive.drawn, m.archive.drawnSchedules = nil, nil
	m.search.drawn = nil
}

// buildFrame builds what the active screen draws from the state the update left.
func (m *model) buildFrame() {
	if builder, ok := m.active().(frameBuilder); ok {
		builder.buildFrame(m)
	}
}

// drawFrame draws the frame the update leaves, keeping the one on screen while the panel waits to be told its size.
func (m *model) drawFrame() {
	if m.configErr != nil || m.startupErr != nil || m.picker.frozen != "" {
		return
	}

	if m.isAwaitingSize() && m.term.drawn != "" {
		return
	}

	m.term.drawn = m.drawInMode(m.findMode())
}
