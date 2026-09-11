package main

import (
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryAgentBelongsToAWorkflowThatExists covers a cast the panel can group, since an agent
// naming no workflow is drawn nowhere.
func TestEveryAgentBelongsToAWorkflowThatExists(t *testing.T) {
	held := newCast()

	known := map[string]bool{}
	for _, workflow := range held.workflows {
		known[workflow.GetId()] = true
	}

	for _, agent := range append(held.agents, held.archivedAgents...) {
		assert.True(t, known[agent.GetWorkflowId()], "%s names workflow %q", agent.GetName(), agent.GetWorkflowId())
	}
}

// TestEveryMailNamesAnAgentInTheCast covers a mailbox whose correspondents can all be drawn by
// name.
func TestEveryMailNamesAnAgentInTheCast(t *testing.T) {
	held := newCast()

	known := map[string]bool{}
	for _, agent := range held.agents {
		known[agent.GetId()] = true
	}

	for _, mail := range held.mail {
		assert.True(t, known[mail.GetToAgentId()], "mail %s is addressed to %q", mail.GetId(), mail.GetToAgentId())

		if from := mail.GetFromAgentId(); from != "" {
			assert.True(t, known[from], "mail %s is from %q", mail.GetId(), from)
		}
	}
}

// TestTheCastCoversEveryStateThePanelDrawsDifferently covers the reason for a cast at all, which
// is that a screen drawn against one bland agent proves nothing.
func TestTheCastCoversEveryStateThePanelDrawsDifferently(t *testing.T) {
	held := newCast()

	seen := map[string]bool{}
	for _, agent := range append(held.agents, held.archivedAgents...) {
		seen[agent.GetState()] = true
	}

	// Kept by hand: nothing fails when an eighth state is added to agentstore, so this list is
	// what somebody has to remember to extend.
	for _, state := range []agentstore.State{
		agentstore.StateThinking,
		agentstore.StateWaiting,
		agentstore.StateIdle,
		agentstore.StateSettingUp,
		agentstore.StateFailedToCreate,
		agentstore.StateFailedToCleanup,
		agentstore.StateUncertain,
	} {
		assert.True(t, seen[string(state)], "no agent is %s", state)
	}
}

// TestTheCastCarriesAReportedReasonLongEnoughToBeClipped covers the widest and only varying part
// of a row, which is what decides whether one fits.
func TestTheCastCarriesAReportedReasonLongEnoughToBeClipped(t *testing.T) {
	held := newCast()

	var longest int

	for _, agent := range append(held.agents, held.archivedAgents...) {
		longest = max(longest, len(agent.GetReportedReason()))
	}

	assert.Greater(t, longest, 60, "no reported reason is long enough to be clipped")
}

// TestTheCastCarriesTheMarksARowCanShow covers unread mail, an unviewed turn, a failed run and a
// hand-set mark all appearing somewhere.
func TestTheCastCarriesTheMarksARowCanShow(t *testing.T) {
	held := newCast()

	var unread, unviewed, failed, marked, child, hidden, scheduled bool

	for _, agent := range held.agents {
		unread = unread || agent.GetUnreadMail()
		unviewed = unviewed || agent.GetUnviewed()
		failed = failed || agent.GetFailedRun()
		marked = marked || agent.GetMark() != ""
		child = child || agent.GetParentAgentId() != ""
		hidden = hidden || agent.GetHidden()
		scheduled = scheduled || agent.GetScheduleId() != ""
	}

	assert.True(t, unread, "no agent has unread mail")
	assert.True(t, unviewed, "no agent has an unviewed turn")
	assert.True(t, failed, "no agent has a failed run")
	assert.True(t, marked, "no agent carries a mark")
	assert.True(t, child, "no agent is a child of another")
	assert.True(t, hidden, "no agent is hidden")
	assert.True(t, scheduled, "no agent was launched by a schedule")
}

// TestAnArchivedAgentIsNotAlive covers a state no real archived agent is in, which would mislead
// whoever writes the next screen against this cast.
func TestAnArchivedAgentIsNotAlive(t *testing.T) {
	for _, agent := range newCast().archivedAgents {
		assert.False(t, agent.GetAlive(), "%s is archived and reads as alive", agent.GetName())
		assert.NotEmpty(t, agent.GetArchivedAt(), "%s is archived and carries no time", agent.GetName())
	}
}

// TestMailIsNumberedOldestFirst covers the mail screen taking id order for time order, which it
// does because a real id is a uuidv7.
func TestMailIsNumberedOldestFirst(t *testing.T) {
	held := newCast().mail
	require.Greater(t, len(held), 1)

	for i := 1; i < len(held); i++ {
		previous, current := held[i-1], held[i]

		assert.Less(t, previous.GetId(), current.GetId(), "the ids are out of order")
		assert.Less(t, previous.GetCreatedAt(), current.GetCreatedAt(),
			"%s sorts before %s but was written after it", previous.GetId(), current.GetId())
	}
}

// TestAWorkflowHoldsNoAgents covers the empty group, which is a row the panel draws and a cast of
// busy workflows never shows.
func TestAWorkflowHoldsNoAgents(t *testing.T) {
	held := newCast()

	populated := map[string]bool{}
	for _, agent := range held.agents {
		populated[agent.GetWorkflowId()] = true
	}

	var empty int

	for _, workflow := range held.workflows {
		if !populated[workflow.GetId()] {
			empty++
		}
	}

	assert.Positive(t, empty, "every workflow holds an agent")
}

// TestBothFailedAgentsCarryTheirReason covers the screen a person opens because an agent failed,
// which reads the reason out of metadata rather than off the row.
func TestBothFailedAgentsCarryTheirReason(t *testing.T) {
	held := newCast()

	var failed int

	for _, agent := range append(held.agents, held.archivedAgents...) {
		if !agentstore.State(agent.GetState()).IsFailed() {
			continue
		}

		failed++

		var reason string

		for _, entry := range agent.GetMetadata() {
			if entry.GetKey() == "error" {
				reason = entry.GetValue()
			}
		}

		assert.NotEmpty(t, reason, "%s failed and records no reason", agent.GetName())
	}

	assert.Equal(t, 2, failed, "the cast should hold a failed create and a failed cleanup")
}
