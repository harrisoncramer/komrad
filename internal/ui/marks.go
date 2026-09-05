package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// markPending is the press the panel is waiting on a letter for, since setting a mark and
// jumping to one are each two keys the way vim's own marks are.
type markPending int

const (
	markPendingNone markPending = iota
	markPendingSet
	markPendingJump
)

// markState is what the panel keeps between the two keys: the press waiting on a letter, and
// the agent the last jump left, which the jump key pressed twice goes back to.
type markState struct {
	pending markPending
	origin  string
}

var (
	errMarkLetter  = errors.New("a mark is one letter, a to z")
	errNoMarkedRow = errors.New("the marked agent has no row on this screen")
)

func (m model) beginMark(pending markPending) (tea.Model, tea.Cmd) {
	m.marks.pending = pending

	return m, nil
}

// readMark takes the key pressed after a mark binding, which is the letter the mark answers to.
func (m model) readMark(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	pending := m.marks.pending
	m.marks.pending = markPendingNone

	switch k := key.String(); {
	case k == "esc":
		return m, nil
	case pending == markPendingJump && isBound(k, m.keys.Marks.Jump):
		return m.jumpMarkBack()
	case !isMarkLetter(k):
		m.report("Could not read the mark", errMarkLetter)

		return m, nil
	case pending == markPendingSet:
		return m.setMark(k)
	default:
		return m.jumpMark(k)
	}
}

func isMarkLetter(key string) bool {
	return len(key) == 1 && key[0] >= 'a' && key[0] <= 'z'
}

// setMark puts a letter on the hovered agent, or takes it off when the agent already answers
// to that letter, which is the way a mark is cleared without a key of its own.
func (m model) setMark(letter string) (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	mark := &letter
	if item.agent.Mark != nil && *item.agent.Mark == letter {
		mark = nil
	}

	return m, m.noticeCmd(fmt.Sprintf("Could not mark agent %q", item.agent.Name), func(a api) (string, error) {
		cleared, err := a.setMark(item.agent.ID, mark)
		if err != nil || len(cleared) == 0 {
			return "", err
		}

		return fmt.Sprintf("Mark %s moved off %s", letter, strings.Join(cleared, ", ")), nil
	})
}

func (m model) jumpMark(letter string) (tea.Model, tea.Cmd) {
	target, ok := m.markedAgent(letter)
	if !ok {
		m.report("Could not jump to the mark", fmt.Errorf("no agent is marked %s", letter))

		return m, nil
	}

	return m.jumpToAgent(target)
}

// jumpMarkBack returns to the agent the last jump left, so a look at a marked agent and the way
// back to what you were reading are the same gesture.
func (m model) jumpMarkBack() (tea.Model, tea.Cmd) {
	if m.marks.origin == "" {
		return m, nil
	}

	return m.jumpToAgent(m.marks.origin)
}

func (m model) markedAgent(letter string) (string, bool) {
	for _, item := range m.listing.agents {
		if item.agent.Mark != nil && *item.agent.Mark == letter {
			return item.agent.ID, true
		}
	}

	return "", false
}

func (m model) jumpToAgent(id string) (tea.Model, tea.Cmd) {
	origin := ""
	if item, ok := m.selectedAgent(); ok {
		origin = item.agent.ID
	}

	key := agentRowKey(id)
	// A filter or a hidden row would make the jump quietly do nothing, so what stands between
	// the cursor and the agent is cleared rather than the jump refusing to land.
	if rowIndexOfKey(m.agentRows(), key) < 0 {
		m.filter = filterState{}
		m.tree.showHidden = true
	}

	idx := rowIndexOfKey(m.agentRows(), key)
	if idx < 0 {
		m.report("Could not jump to the mark", errNoMarkedRow)

		return m, nil
	}

	m.marks.origin = origin

	return m.jumpCursor(idx), nil
}
