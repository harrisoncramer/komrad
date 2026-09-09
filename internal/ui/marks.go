package ui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// markPending is the press the panel is waiting on a letter for.
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
	errNoMarkedRow = errors.New("the marked row is not on this screen")
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

// setMark puts a letter on the row under the cursor, or takes it off when that row already
// answers to the letter, which is the way a mark is cleared without a key of its own.
func (m model) setMark(letter string) (tea.Model, tea.Cmd) {
	row, ok := m.hoveredRow()
	if !ok {
		return m, nil
	}

	key := rowKey(row)
	target := markTarget(row)
	failure := fmt.Sprintf("Could not mark %s", describeRow(row))

	if m.listing.marks[key] == letter {
		return m, m.writeCmd(failure, func(a api) error { return a.clearMark(target) })
	}

	return m, m.writeCmd(failure, func(a api) error { return a.setMark(target, letter) })
}

// markTarget is the row a mark would go on, which the panel names by kind since a letter sits
// on an agent or on a workflow.
func markTarget(row agentRow) markTargetRef {
	if row.header {
		return markTargetRef{workflowID: row.workflow.ID}
	}

	return markTargetRef{agentID: row.item.agent.ID}
}

type markTargetRef struct {
	agentID    string
	workflowID string
}

func describeRow(row agentRow) string {
	if row.header {
		return fmt.Sprintf("workflow %q", row.workflow.Name)
	}

	return fmt.Sprintf("agent %q", row.item.agent.Name)
}

func (m model) jumpMark(letter string) (tea.Model, tea.Cmd) {
	target, ok := m.markedRow(letter)
	if !ok {
		m.report("Could not jump to the mark", fmt.Errorf("nothing is marked %s", letter))

		return m, nil
	}

	next, cmd, ok := m.jumpToRow(target)
	if !ok {
		m.report("Could not jump to the mark", errNoMarkedRow)

		return m, nil
	}

	return next, cmd
}

// jumpMarkBack returns to the row the last jump left, so a look at a marked agent and the way
// back to what you were reading are the same gesture.
func (m model) jumpMarkBack() (tea.Model, tea.Cmd) {
	if m.marks.origin == "" {
		return m, nil
	}

	next, cmd, ok := m.jumpToRow(m.marks.origin)
	if !ok {
		// The row has been archived or deleted since the jump, and keeping the id would leave
		// the key failing the same way for the rest of the session.
		m.marks.origin = ""
		m.report("Could not jump back", errOriginGone)

		return m, nil
	}

	return next, cmd
}

// markedRow is the row a letter names, read down the listing rather than out of the map, so a
// letter briefly on two rows between passes picks the same one every time, and so a row the tree
// is not drawing is still somewhere a jump can reach.
func (m model) markedRow(letter string) (string, bool) {
	for _, item := range m.listing.agents {
		if key := agentRowKey(item.agent.ID); m.listing.marks[key] == letter {
			return key, true
		}
	}

	for _, workflow := range m.listing.workflows {
		if key := workflowRowKey(workflow.ID); m.listing.marks[key] == letter {
			return key, true
		}
	}

	return "", false
}

// jumpToRow puts the cursor on a row named by its key, recording the row it left so the jump
// can be undone, and reports whether the row was reachable at all.
func (m model) jumpToRow(key string) (model, tea.Cmd, bool) {
	origin := rowKeyAt(m.agentRows(), m.tree.cursor)

	revealed := m
	// A hidden row would make the jump quietly do nothing, so it is revealed on a copy the caller
	// drops when the row is out of reach for some other reason, and a search is left alone
	// throughout since a search never took a row off the screen to begin with.
	if rowIndexOfKey(revealed.agentRows(), key) < 0 {
		revealed.tree.showHidden = true
	}

	idx := rowIndexOfKey(revealed.agentRows(), key)
	if idx < 0 {
		return m, nil, false
	}

	revealed.marks.origin = origin

	next, cmd := revealed.jumpCursor(idx)

	return next, cmd, true
}
