package ui

// frameBuilder is a screen that builds what its frame renders once an update has settled.
type frameBuilder interface {
	buildFrame(m *model)
}

// dropFrames forgets what the screens built for the last frame, so an update reads the state it changes.
func (m *model) dropFrames() {
	m.agents.rendered = nil
}

// buildFrame builds what the active screen renders from the state the update left.
func (m *model) buildFrame() {
	if builder, ok := m.active().(frameBuilder); ok {
		builder.buildFrame(m)
	}
}

type shownScreen struct {
	screen    screen
	agents    agentsKind
	schedules schedulesKind
}

func (m *model) shownScreen() shownScreen {
	return shownScreen{screen: m.screen, agents: m.agents.kind, schedules: m.schedules.kind}
}

func (m *model) forgetDetailsFit() {
	m.detailsFits = derived[shownScreen, bool]{}
}

// dropRenderedView forgets the frame View last rendered, keeping it while the panel waits to be told its size.
func (m *model) dropRenderedView() {
	if m.isAwaitingSize() {
		return
	}

	m.term.renderedView = ""
}
