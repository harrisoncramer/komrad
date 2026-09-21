package ui

import (
	"context"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/key"
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
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{{agent: panel.Agent{
				ID:         "a1",
				Name:       "telescope",
				State:      panel.StateThinking,
				WorkflowID: "w1",
				HeldAt:     held,
			}, alive: true}},
		},
	}
	m.agents.cursor = firstAgentRowIndex(m.agents.rows(m.shared()))

	return m
}

// TestUnpausingTellsTheAgentToCarryOn pins the panel against a pause that has no way out: release
// only types at an agent when it is given something to say, so a silent one would stop it for good.
func TestUnpausingTellsTheAgentToCarryOn(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)

	pressed, cmd := m.Update(keyMsg("p"))
	written, ok := pressed.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	require.Len(t, agents.released, 1)
	assert.Equal(t, "a1", agents.released[0].GetId())
	assert.NotEmpty(t, agents.released[0].GetSaid(), "an unpaused agent is told to carry on rather than left stopped")
}

// TestPausingSendsNoWordsToTheAgent is the other half: taking the tree is not a conversation.
func TestPausingSendsNoWordsToTheAgent(t *testing.T) {
	agents := &recordingHolds{}

	m := heldModel(agents, nil)

	pressed, cmd := m.Update(keyMsg("p"))
	written, ok := pressed.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	assert.Equal(t, []string{"a1"}, agents.held)
	assert.Empty(t, agents.released)
}

// TestOpenKeyDoesNothingForHeldAgent covers the open key while an agent is held.
func TestOpenKeyDoesNothingForHeldAgent(t *testing.T) {
	at := time.Now()
	tmux := &recordingTmux{}

	m := model{
		api:      api{agents: &recordingHolds{}, workflows: emptyLists{}},
		tmux:     tmux,
		settings: PanelSettings{Pane: "%3"},
		frame:    layout.New(tmux, config.DefaultLayout()),
		keys:     config.DefaultKeybindings(),
		screen:   screenAgents,
		listing: listing{
			workflows: []panel.Workflow{{
				ID:   "w1",
				Name: "backend",
			}},
			agents: []agentItem{{
				agent: panel.Agent{
					ID:         "a1",
					Name:       "telescope",
					State:      panel.StateThinking,
					WorkflowID: "w1",
					Pane:       "%9",
					HeldAt:     &at,
				},
				alive: true,
			}},
		},
	}
	m.agents.cursor = firstAgentRowIndex(m.agents.rows(m.shared()))
	m.frame.Attach("%3")

	m.agents.openAgent(&m, m.shared())
	got := m

	assert.Empty(t, tmux.selected, "held agent pane not focused")
	assert.Empty(t, got.frame.GuestPane(), "held agent pane not shown as guest")
	assert.Equal(t, screenAgents, got.screen)
}

// TestHeldAgentShowsDocNotPane covers a held agent drawing its document in place of its pane.
func TestHeldAgentShowsDocNotPane(t *testing.T) {
	at := time.Now()

	m := heldModel(&recordingHolds{}, &at)

	assert.True(t, m.agents.hasNoAgentPane(m.shared()), "held agent treated as having no pane")
	assert.Equal(t, heldPlaceholder, m.agents.doc(m.shared()), "held agent shows paused placeholder")
}

// TestAPausedRowSaysWhichThingIsTrue pins what a paused row draws in each case.
func TestAPausedRowSaysWhichThingIsTrue(t *testing.T) {
	at := time.Now()

	held := func(state panel.State) agentItem {
		return agentItem{agent: panel.Agent{
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
		{
			name:  "at rest, which is what a pause claims",
			item:  held(panel.StateIdle),
			wants: heldNotice,
		},
		{
			name:  "writing again",
			item:  held(panel.StateThinking),
			wants: heldWorkingNotice,
		},
		{
			name:  "stopped at a prompt anybody could answer",
			item:  held(panel.StateWaiting),
			wants: heldPromptedNotice,
		},
		{
			name:  "a pane nobody can read",
			item:  held(panel.StateUncertain),
			wants: heldUnreadableNotice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			said := noticeTexts(agentNotices(noticeRow(tt.item)))

			assert.Contains(t, said, tt.wants)
			assert.Len(t, heldSaid(said), 1, "a row says one thing about a pause, not two")
		})
	}
}

// heldSaid is the notices about the pause itself.
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
	said := xslice.Map(notices, func(n notice) string {
		return n.text
	})

	return said
}

// TestUnpausingInSilenceLetsAGoneBoxGo covers the only way out of a pause whose agent has an
// input box that will never take the words, which leaves the unpause key refusing forever.
func TestUnpausingInSilenceLetsAGoneBoxGo(t *testing.T) {
	tests := []struct {
		name     string
		answer   string
		wantSent int
	}{
		{
			name:     "confirming lets it go without saying anything",
			answer:   "y",
			wantSent: 1,
		},
		{name: "declining keeps the hold", answer: "n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := time.Now()
			agents := &recordingHolds{}

			m := heldModel(agents, &at)

			pressed, _ := m.Update(keyMsg("P"))
			opened, ok := pressed.(model)
			require.True(t, ok)
			require.Equal(t, formAgentRelease, opened.form.kind, "the key asks before it drops the words")

			got := answerPrompt(t, opened, tt.answer)

			require.Len(t, agents.released, tt.wantSent)
			if tt.wantSent > 0 {
				assert.Equal(t, "a1", agents.released[0].GetId())
				assert.Empty(t, agents.released[0].GetSaid(), "nothing is typed at an agent let go in silence")
			}

			assert.False(t, got.form.isActive(), "the prompt is down")
		})
	}
}

// TestUnpausingInSilenceIsOnlyForAHeldAgent covers the key on a row nobody is holding, which has
// no hold to get out of.
func TestUnpausingInSilenceIsOnlyForAHeldAgent(t *testing.T) {
	agents := &recordingHolds{}

	m := heldModel(agents, nil)

	pressed, cmd := m.Update(keyMsg("P"))
	written, ok := pressed.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	assert.Equal(t, formNone, written.form.kind)
	assert.Empty(t, agents.released)
}

// TestAPausedRowNamesTheWayOutOfThePause covers the legend a person holding only the panel reads to
// find the release that says nothing.
func TestAPausedRowNamesTheWayOutOfThePause(t *testing.T) {
	at := time.Now()

	m := heldModel(&recordingHolds{}, &at)

	descs := xslice.Map(m.agentHelp().full, func(b key.Binding) string {
		return b.Help().Key + " " + b.Help().Desc
	})

	assert.Contains(t, descs, "P unpause in silence")

	loose := heldModel(&recordingHolds{}, nil)

	others := xslice.Map(loose.agentHelp().full, func(b key.Binding) string {
		return b.Help().Desc
	})

	assert.NotContains(t, others, "unpause in silence", "a row nobody is holding has no pause to get out of")
}
