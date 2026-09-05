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
// the row the last jump left, which the jump key pressed twice goes back to.
type markState struct {
	pending markPending
	origin  string
}

var (
	errMarkLetter  = errors.New("a mark is one letter, a to z")
	errNoMarkedRow = errors.New("the marked agent has no row on this screen")
	errOriginGone  = errors.New("the row you jumped from is no longer on this screen")
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

	next, ok := m.jumpToRow(agentRowKey(target))
	if !ok {
		m.report("Could not jump to the mark", errNoMarkedRow)

		return m, nil
	}

	return next, nil
}

// jumpMarkBack returns to the row the last jump left, so a look at a marked agent and the way
// back to what you were reading are the same gesture.
func (m model) jumpMarkBack() (tea.Model, tea.Cmd) {
	if m.marks.origin == "" {
		return m, nil
	}

	next, ok := m.jumpToRow(m.marks.origin)
	if !ok {
		// The row has been archived or deleted since the jump, and keeping the id would leave
		// the key failing the same way for the rest of the session.
		m.marks.origin = ""
		m.report("Could not jump back", errOriginGone)

		return m, nil
	}

	return next, nil
}

func (m model) markedAgent(letter string) (string, bool) {
	for _, item := range m.listing.agents {
		if item.agent.Mark != nil && *item.agent.Mark == letter {
			return item.agent.ID, true
		}
	}

	return "", false
}

// jumpToRow puts the cursor on a row named by its key, recording the row it left so the jump
// can be undone, and reports whether the row was reachable at all.
func (m model) jumpToRow(key string) (model, bool) {
	origin := rowKeyAt(m.agentRows(), m.tree.cursor)

	revealed := m
	// A filter or a hidden row would make the jump quietly do nothing, so what stands between the
	// cursor and the row is cleared on a copy the caller drops when the row is out of reach for
	// some other reason, rather than losing the screen on the way to nowhere.
	if rowIndexOfKey(revealed.agentRows(), key) < 0 {
		revealed.filter = filterState{}
		revealed.tree.showHidden = true
	}

	idx := rowIndexOfKey(revealed.agentRows(), key)
	if idx < 0 {
		return m, false
	}

	revealed.marks.origin = origin

	return revealed.jumpCursor(idx), true
}
