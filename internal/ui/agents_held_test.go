package ui

import (
	"context"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHolds answers the hold calls and keeps what the panel sent.
type recordingHolds struct {
	agentv1connect.AgentServiceClient
	held     []string
	released []*agentv1.ReleaseAgentRequest
}

func (r *recordingHolds) HoldAgent(_ context.Context, req *connect.Request[agentv1.HoldAgentRequest]) (*connect.Response[agentv1.HoldAgentResponse], error) {
	r.held = append(r.held, req.Msg.GetId())

	return connect.NewResponse(&agentv1.HoldAgentResponse{}), nil
}

func (r *recordingHolds) ReleaseAgent(_ context.Context, req *connect.Request[agentv1.ReleaseAgentRequest]) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	r.released = append(r.released, req.Msg)

	return connect.NewResponse(&agentv1.ReleaseAgentResponse{}), nil
}

func (r *recordingHolds) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// heldModel is the agents screen standing on one agent, held or not.
func heldModel(agents agentv1connect.AgentServiceClient, held *time.Time) model {
	m := model{
		api:    api{agents: agents, workflows: emptyLists{}},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "telescope",
				State:      agentstore.StateThinking,
				WorkflowID: "w1",
				HeldAt:     held,
			}, alive: true}},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	return m
}

// TestLettingGoTellsTheAgentToCarryOn pins the panel against a hold that has no way out: release
// only types at an agent when it is given something to say, so a silent one would stop it for good.
func TestLettingGoTellsTheAgentToCarryOn(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)

	pressed, cmd := m.Update(keyMsg("p"))
	written, ok := pressed.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	require.Len(t, agents.released, 1)
	assert.Equal(t, "a1", agents.released[0].GetId())
	assert.NotEmpty(t, agents.released[0].GetSaid(), "an agent let go is told to carry on rather than left stopped")
}

// TestHoldingSendsNoWordsToTheAgent is the other half: taking the tree is not a conversation.
func TestHoldingSendsNoWordsToTheAgent(t *testing.T) {
	agents := &recordingHolds{}

	m := heldModel(agents, nil)

	pressed, cmd := m.Update(keyMsg("p"))
	written, ok := pressed.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	assert.Equal(t, []string{"a1"}, agents.held)
	assert.Empty(t, agents.released)
}

// Explainer: a hold is taken on one reading, so an agent that was about to start a turn reads
// like one that has stopped, and a pane that goes unreadable afterwards says nothing about
// whether it stopped at all. The row admits both rather than letting go on the person's behalf.

// TestAHeldRowSaysWhichThingIsTrue pins what a held row draws in each case.
func TestAHeldRowSaysWhichThingIsTrue(t *testing.T) {
	at := time.Now()

	held := func(state agentstore.State) agentItem {
		return agentItem{agent: agentstore.Agent{
			Name:   "telescope",
			State:  state,
			HeldAt: &at,
		}, alive: true}
	}

	tests := []struct {
		name  string
		item  agentItem
		wants string
	}{
		{name: "at rest, which is what a hold claims", item: held(agentstore.StateIdle), wants: heldNotice},
		{name: "writing again", item: held(agentstore.StateThinking), wants: heldWorkingNotice},
		{name: "stopped at a prompt anybody could answer", item: held(agentstore.StateWaiting), wants: heldPromptedNotice},
		{name: "a pane nobody can read", item: held(agentstore.StateUncertain), wants: heldUnreadableNotice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			said := noticeTexts(agentNotices(tt.item))

			assert.Contains(t, said, tt.wants)
			assert.Len(t, heldSaid(said), 1, "a row says one thing about a hold, not two")
		})
	}
}

// heldSaid is the notices about the hold itself, which is what a row may only say once.
func heldSaid(said []string) []string {
	kept := make([]string, 0, len(said))

	for _, one := range said {
		if one == heldNotice || one == heldWorkingNotice || one == heldPromptedNotice || one == heldUnreadableNotice {
			kept = append(kept, one)
		}
	}

	return kept
}

// noticeTexts is what a row's notices say, without their icons.
func noticeTexts(notices []notice) []string {
	said := make([]string, 0, len(notices))
	for _, n := range notices {
		said = append(said, n.text)
	}

	return said
}
