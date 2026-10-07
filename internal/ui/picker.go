package ui

import (
	"slices"

	"komrad/internal/panes"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
)

// pickLetters are the keys the picker hands out, one per target in reading order.
var pickLetters = []string{"d", "f", "j", "k", "l", "s", "a"}

// pickerState is the window as the pane picker froze it.
type pickerState struct {
	// freezing is whether a snapshot is being taken.
	freezing bool
	// frozen is the window rendered as it stood, empty while nothing is frozen.
	frozen string
	// returnTo is the pane that held the keys before the window froze.
	returnTo string
	targets  []pickTarget
}

func (p pickerState) isActive() bool {
	return p.freezing || p.frozen != ""
}

// pickTarget is one place the picker can land the keys, in the window's cells.
type pickTarget struct {
	letter string
	rect   panes.Rect
	// pane is the tmux pane to select, empty for a block inside the sidebar.
	pane string
	// stop is the sidebar block landed on when pane is empty.
	stop stop
}

// pickerFrozenMsg is the snapshot of the window, taken off the update loop.
type pickerFrozenMsg struct {
	frozen   string
	returnTo string
	targets  []pickTarget
}

// routePicker answers every key while the picker holds the window.
func (m *model) routePicker(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.picker.isActive() {
		return nil, false
	}

	if m.picker.freezing {
		return nil, true
	}

	pressed := key.String()
	if pressed == "esc" || pressed == "q" {
		m.thaw()

		return nil, true
	}

	at := slices.IndexFunc(m.picker.targets, func(t pickTarget) bool { return t.letter == pressed })
	if at < 0 {
		return nil, true
	}

	return m.land(m.picker.targets[at]), true
}

func (m *model) freeze() tea.Cmd {
	m.picker = pickerState{freezing: true}

	blocks := m.findSidebarBlocks()
	sidebar := m.settings.Pane
	tmux := m.tmux
	palette := paint.Panel
	labelEnd := m.findModeEnd()

	return func() tea.Msg {
		returnTo := tmux.GetActivePane(sidebar)
		shown := tmux.GetShownPanes(sidebar)

		screens := make(map[string]string, len(shown))
		for _, rect := range shown {
			screens[rect.Pane] = tmux.CaptureScreen(rect.Pane)
		}

		window := frozenWindow{
			shown:    shown,
			screens:  screens,
			sidebar:  sidebar,
			labelEnd: labelEnd,
			palette:  palette,
		}
		targets := findPickTargets(shown, sidebar, blocks)

		return pickerFrozenMsg{
			frozen:   window.renderPicker(targets),
			returnTo: returnTo,
			targets:  targets,
		}
	}
}

// findPickTargets letters the sidebar's blocks and then every other pane on show, in reading order.
func findPickTargets(shown []panes.Rect, sidebar string, blocks []sidebarBlock) []pickTarget {
	var targets []pickTarget

	others := slices.Clone(shown)
	slices.SortFunc(others, func(a, b panes.Rect) int {
		if a.Left != b.Left {
			return a.Left - b.Left
		}

		return a.Top - b.Top
	})

	for _, rect := range others {
		if rect.Pane != sidebar {
			continue
		}

		for _, block := range blocks {
			clipped, ok := clipRect(block.rect, panes.Rect{Width: rect.Width, Height: rect.Height})
			if !ok {
				continue
			}

			clipped.Left += rect.Left
			clipped.Top += rect.Top
			targets = append(targets, pickTarget{rect: clipped, stop: block.stop})
		}
	}

	for _, rect := range others {
		if rect.Pane != sidebar {
			targets = append(targets, pickTarget{rect: rect, pane: rect.Pane})
		}
	}

	if len(targets) > len(pickLetters) {
		targets = targets[:len(pickLetters)]
	}

	for i := range targets {
		targets[i].letter = pickLetters[i]
	}

	return targets
}

func (m *model) applyFreeze(msg pickerFrozenMsg) {
	if !m.picker.freezing {
		return
	}

	if msg.frozen == "" {
		m.picker = pickerState{}

		return
	}

	m.picker = pickerState{
		frozen:   msg.frozen,
		returnTo: msg.returnTo,
		targets:  msg.targets,
	}
	m.frame.Freeze()
}

func (m *model) thaw() {
	returnTo := m.picker.returnTo
	m.unfreeze()

	if returnTo != "" && returnTo != m.settings.Pane {
		m.tmux.SelectPane(returnTo)
	}
}

// land thaws the window and hands the keys to a target.
func (m *model) land(target pickTarget) tea.Cmd {
	m.unfreeze()

	if target.pane != "" {
		m.tmux.SelectPane(target.pane)
		m.term.keysElsewhere = target.pane != m.settings.Pane

		return nil
	}

	m.term.keysElsewhere = false

	return m.enterStop(target.stop)
}

func (m *model) unfreeze() {
	m.picker = pickerState{}
	m.frame.Thaw()
}
