package main

import (
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
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
	for _, agent := range held.agents {
		seen[agent.GetState()] = true
	}

	for _, state := range []agentstore.State{
		agentstore.StateThinking,
		agentstore.StateIdle,
		agentstore.StateWaiting,
		agentstore.StateUncertain,
	} {
		assert.True(t, seen[string(state)], "no agent is %s", state)
	}
}

// TestTheCastCarriesTheMarksARowCanShow covers unread mail, an unviewed turn, a failed run and a
// hand-set mark all appearing somewhere.
func TestTheCastCarriesTheMarksARowCanShow(t *testing.T) {
	held := newCast()

	var unread, unviewed, failed, marked, child bool

	for _, agent := range held.agents {
		unread = unread || agent.GetUnreadMail()
		unviewed = unviewed || agent.GetUnviewed()
		failed = failed || agent.GetFailedRun()
		marked = marked || agent.GetMark() != ""
		child = child || agent.GetParentAgentId() != ""
	}

	assert.True(t, unread, "no agent has unread mail")
	assert.True(t, unviewed, "no agent has an unviewed turn")
	assert.True(t, failed, "no agent has a failed run")
	assert.True(t, marked, "no agent carries a mark")
	assert.True(t, child, "no agent is a child of another")
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
