package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// markPending is the press the panel is waiting on a letter for.
type markPending int

const (
	markPendingNone markPending = iota
	markPendingSet
	markPendingJump
)

// markState is what the panel keeps between the two keys: the press waiting on a letter, and the
// row the last jump left, which the jump key pressed twice goes back to.
type markState struct {
	pending markPending
	origin  string
}

var (
	errMarkLetter  = errors.New("a mark is one letter, a to z")
	errNoMarkedRow = errors.New("the marked row is not on this screen")
	errOriginGone  = errors.New("the row you jumped from is no longer on this screen")
)

func (a *agentsScreen) beginMark(pending markPending) tea.Cmd {
	a.marks.pending = pending

	return nil
}

// isMarkPending reports whether a mark binding is waiting on the letter after it.
func (a *agentsScreen) isMarkPending() bool {
	return a.marks.pending != markPendingNone
}

// readMark takes the key pressed after a mark binding, being the letter the mark answers to.
func (a *agentsScreen) readMark(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	pending := a.marks.pending
	a.marks.pending = markPendingNone

	switch k := key.String(); {
	case k == "esc":
		return nil
	case pending == markPendingJump && isBound(k, s.keys.Marks.Jump):
		return a.jumpMarkBack(h, s)
	case !isMarkLetter(k):
		h.report("Could not read the mark", errMarkLetter)

		return nil
	case pending == markPendingSet:
		return a.setMark(h, s, k)
	default:
		return a.jumpMark(h, s, k)
	}
}

func isMarkLetter(key string) bool {
	return len(key) == 1 && key[0] >= 'a' && key[0] <= 'z'
}

// setMark puts a letter on the row under the cursor, or takes it off when that row already
// answers to the letter.
func (a *agentsScreen) setMark(h host, s panelShared, letter string) tea.Cmd {
	row, ok := a.hoveredRow(s)
	if !ok {
		return nil
	}

	target := markTarget(row)
	failure := fmt.Sprintf("Could not mark %s", describeRow(row))

	if s.listing.marks[rowID(row)] == letter {
		return h.writeCmd(failure, func(c api) error { return c.clearMark(target) })
	}

	return h.writeCmd(failure, func(c api) error { return c.setMark(target, letter) })
}

// markTarget is the row a mark would go on, named by kind since a letter sits on an agent or on
// a workflow.
func markTarget(row agentRow) markTargetRef {
	if row.header {
		return markTargetRef{workflowID: row.workflow.ID}
	}

	return markTargetRef{agentID: row.item.Agent.ID}
}

type markTargetRef struct {
	agentID    string
	workflowID string
}

func describeRow(row agentRow) string {
	if row.header {
		return fmt.Sprintf("workflow %q", row.workflow.Name)
	}

	return fmt.Sprintf("agent %q", row.item.Agent.Name)
}

func (a *agentsScreen) jumpMark(h host, s panelShared, letter string) tea.Cmd {
	target, ok := markedRow(s.listing, letter)
	if !ok {
		h.report("Could not jump to the mark", fmt.Errorf("nothing is marked %s", letter))

		return nil
	}

	cmd, ok := a.jumpToRow(h, s, target)
	if !ok {
		h.report("Could not jump to the mark", errNoMarkedRow)

		return nil
	}

	return cmd
}

// jumpMarkBack returns to the row the last jump left, so a look at a marked agent and the way
// back to what you were reading are the same gesture.
func (a *agentsScreen) jumpMarkBack(h host, s panelShared) tea.Cmd {
	if a.marks.origin == "" {
		return nil
	}

	cmd, ok := a.jumpToRow(h, s, a.marks.origin)
	if !ok {
		// The row has been archived or deleted since the jump, and keeping the id would leave
		// the key failing the same way for the rest of the session.
		a.marks.origin = ""
		h.report("Could not jump back", errOriginGone)

		return nil
	}

	return cmd
}

// markedRow is the row a letter names, read down the listing rather than out of the map, so a
// letter briefly on two rows between passes picks the same one every time.
func markedRow(l listing, letter string) (string, bool) {
	for _, item := range l.agents {
		if l.marks[item.Agent.ID] == letter {
			return agentRowKey(item.Agent.ID), true
		}
	}

	for _, workflow := range l.workflows {
		if l.marks[workflow.ID] == letter {
			return workflowRowKey(workflow.ID), true
		}
	}

	return "", false
}

// jumpToRow puts the cursor on a row named by its key, recording the row it left so the jump can
// be undone, and reports whether the row was reachable at all.
func (a *agentsScreen) jumpToRow(h host, s panelShared, key string) (tea.Cmd, bool) {
	origin := a.list().hovered(s)

	revealed, shown := *a, s
	// A hidden row, one a filter took off the screen, or a workflow a flat list draws no row
	// for, would make the jump quietly do nothing, so each is revealed on a copy the caller
	// drops when the row is out of reach for some other reason.
	if strings.HasPrefix(key, workflowRowPrefix) {
		revealed.sort.flat = false
	}
	if rowIndexOfKey(revealed.rows(shown), key) < 0 {
		shown.chrome.filter.mode = filterModeSearch
	}
	if rowIndexOfKey(revealed.rows(shown), key) < 0 {
		shown.chrome.filter.mode = s.chrome.filter.mode
		revealed.showHidden = true
	}
	if rowIndexOfKey(revealed.rows(shown), key) < 0 {
		shown.chrome.filter.mode = filterModeSearch
	}

	idx := rowIndexOfKey(revealed.rows(shown), key)
	if idx < 0 {
		return nil, false
	}

	*a = revealed
	a.marks.origin = origin
	if shown.chrome.filter.mode != s.chrome.filter.mode {
		h.widenFilter()
	}

	return a.list().jump(h, shown, idx), true
}
