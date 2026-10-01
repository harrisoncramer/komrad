package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

// TestTriageRows covers which agents the zone draws and in what order.
func TestTriageRows(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		rows     []proto.Message
		want     []string
	}{
		{
			name:     "a zone with no rows is not drawn",
			capacity: 0,
			rows:     buildTriage(),
			want:     []string{"backend", "third", "asker", "broken", "quiet"},
		},
		{
			name:     "a zone with nothing in triage is not drawn",
			capacity: 2,
			rows:     []proto.Message{newLiveWorkflow("w1", "backend"), newTriageAgent("quiet", agentv1.AgentState_AGENT_STATE_IDLE, 1)},
			want:     []string{"backend", "quiet"},
		},
		{
			name:     "an orchestrator is named for its workflow",
			capacity: 2,
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				func() *agentv1.Agent {
					orchestrator := newOrchestrator()
					orchestrator.State = agentv1.AgentState_AGENT_STATE_WAITING

					return orchestrator
				}(),
			},
			want: []string{"backend", "backend"},
		},
		{
			name:     "a parent in triage brings its children with it",
			capacity: 2,
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgentAt("quiet", "w1", 1),
				newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
				newChild("reviewer", "reviewer", "asker"),
			},
			want: []string{"asker", "└ reviewer", "backend", "quiet"},
		},
		{
			name:     "a hidden child counts on its parent in triage",
			capacity: 2,
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgentAt("quiet", "w1", 1),
				newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
				func() *agentv1.Agent {
					reviewer := newChild("reviewer", "reviewer", "asker")
					reviewer.Hidden = true

					return reviewer
				}(),
			},
			want: []string{"asker +1", "backend", "quiet"},
		},
		{
			name:     "a child in triage brings its parent with it",
			capacity: 2,
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgentAt("quiet", "w1", 1),
				newTriageAgent("parent", agentv1.AgentState_AGENT_STATE_IDLE, 2),
				func() *agentv1.Agent {
					reviewer := newTriageAgent("reviewer", agentv1.AgentState_AGENT_STATE_WAITING, 3)
					reviewer.ParentAgentId = xptr.Ptr("parent")

					return reviewer
				}(),
			},
			want: []string{"parent", "└ reviewer", "backend", "quiet"},
		},
		{
			name:     "the most urgent tier takes the slots and the rest wait in the tree",
			capacity: 2,
			rows:     buildTriage(),
			want:     []string{"asker", "third", "+1 more waiting for a slot", "backend", "broken", "quiet"},
		},
		{
			name:     "within a tier the agent waiting longest comes first",
			capacity: 3,
			rows:     buildTriage(),
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			assert.Equal(t, tt.want, readTriageList(harness.readFrame()))
		})
	}
}

// TestTriageJump covers the triage key putting the cursor on the zone's first agent.
func TestTriageJump(t *testing.T) {
	tests := []struct {
		name  string
		rows  []proto.Message
		press []string
		want  string
		note  string
	}{
		{
			name:  "with nothing in triage the key says so and stays in the tree",
			rows:  []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("quiet", "w1", 1)},
			press: []string{"t"},
			want:  "quiet",
			note:  "nothing to triage",
		},
		{
			name:  "the key puts the cursor on the first agent in triage",
			rows:  buildTriage(),
			press: []string{"G", "t"},
			want:  "asker",
		},
		{
			name:  "the cursor moves on out of the zone into the tree",
			rows:  buildTriage(),
			press: []string{"t", "j", "j"},
			want:  "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, 2, tt.rows...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.want, strings.TrimSpace(readHoveredRow(frame)))
			assert.Contains(t, frame, tt.note)
		})
	}
}

// TestTriageSnooze covers sinking a family to the bottom of the zone until what it waits on
// changes, as the zone stands once the cursor has left it.
func TestTriageSnooze(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		rows     []proto.Message
		press    []string
		// relisted is the listing the watch sends after the keys, none when it sends nothing.
		relisted []proto.Message
		// then is a second listing the watch sends after relisted, none when it sends nothing.
		then []proto.Message
		// want is the list as drawn, unchecked when nil.
		want []string
	}{
		{
			name:     "a family whose head was archived comes back unsnoozed",
			capacity: 3,
			rows:     buildTriageFamily(),
			press:    []string{"t", "space", "z"},
			relisted: []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("quiet", "w1", 1), buildTriageFamily()[4]},
			then:     buildTriageFamily(),
			want:     []string{"parent", "└ reviewer", "third", "backend", "quiet"},
		},
		{
			name:     "a family hidden and shown again keeps its snooze",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: func() []proto.Message {
				rows := buildTriage()
				rows[2].(*agentv1.Agent).Hidden = true

				return rows
			}(),
			then: buildTriage(),
			want: []string{"third", "broken", "asker", "backend", "quiet"},
		},
		{
			name:     "an agent that asks again ends its snooze",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: func() []proto.Message {
				rows := buildTriage()
				rows[2].(*agentv1.Agent).StateChangedAt = "2026-09-20T13:06:00Z"

				return rows
			}(),
			want: []string{"asker", "third", "broken", "backend", "quiet"},
		},
		{
			name:     "an agent that moves tier ends its snooze",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: func() []proto.Message {
				rows := buildTriage()
				rows[2] = newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_ERRORED, 1)

				return rows
			}(),
			want: []string{"third", "asker", "broken", "backend", "quiet"},
		},
		{
			name:     "opening an agent that asks leaves it where it is",
			capacity: 3,
			rows:     buildFinished(),
			press:    []string{"t", "enter"},
			want:     []string{"asker", "done", "later", "backend", "quiet"},
		},
		{
			name:     "waking a snoozed family puts it back in its place",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z", "space", "z"},
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
		},
		{
			name:     "a snoozed family gives up its slot",
			capacity: 2,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			want:     []string{"third", "broken", "+1 more waiting for a slot", "backend", "asker", "quiet"},
		},
		{
			name:     "a snooze outlasts a listing that changes nothing",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: buildTriage(),
			want:     []string{"third", "broken", "asker", "backend", "quiet"},
		},
		{
			name:     "opening a finished agent snoozes it",
			capacity: 3,
			rows:     buildFinished(),
			press:    []string{"t", "j", "enter"},
			want:     []string{"asker", "later", "done", "backend", "quiet"},
		},
		{
			name:     "a snoozed family sinks below the rest",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			want:     []string{"third", "broken", "asker", "backend", "quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			harness.press(tt.press...)
			if tt.relisted != nil {
				harness.watch(buildSnapshot(tt.relisted...))
			}
			if tt.then != nil {
				harness.watch(buildSnapshot(tt.then...))
			}
			harness.press("G")

			assert.Equal(t, tt.want, readTriageList(harness.readFrame()))
		})
	}
}

// TestTriageFreeze covers the zone holding still while a person reads it and settling once they
// leave.
func TestTriageFreeze(t *testing.T) {
	withoutThird := func() []proto.Message { return buildTriage()[:4] }
	tests := []struct {
		name     string
		capacity int
		rows     []proto.Message
		// blur says the keys go to the hovered agent's pane before the listing moves.
		blur     bool
		relisted []proto.Message
		// press are the keys pressed once the listing has moved.
		press []string
		// then is a second listing the watch sends after the keys, none when it sends nothing.
		then []proto.Message
		want []string
		// hovered is the row the cursor is on once the zone has settled or held.
		hovered string
	}{
		{
			name:     "an agent that needs triage while the zone is read waits for a slot",
			capacity: 3,
			rows:     withoutThird(),
			relisted: buildTriage(),
			want:     []string{"asker", "broken", "+1 more waiting for a slot", "backend", "third", "quiet"},
			hovered:  "asker",
		},
		{
			name:     "a slot freed while the zone is read stays empty",
			capacity: 2,
			rows:     buildTriage(),
			relisted: func() []proto.Message {
				rows := buildTriage()
				rows[4] = newTriageAgent("third", agentv1.AgentState_AGENT_STATE_THINKING, 4)

				return rows
			}(),
			want:    []string{"asker", "+1 more waiting for a slot", "backend", "third", "broken", "quiet"},
			hovered: "asker",
		},
		{
			name:     "a family snoozed while the zone is read keeps its place",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"space", "z"},
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
			hovered:  "asker",
		},
		{
			name:     "the last agent in triage archived lands the cursor in the tree",
			capacity: 3,
			rows:     buildTriage()[:3],
			relisted: buildTriage()[:2],
			hovered:  "quiet",
		},
		{
			name:     "a family archived from the bottom of the zone lands the cursor on the one above",
			capacity: 3,
			rows:     append(buildTriageFamily()[:4], newTriageAgent("early", agentv1.AgentState_AGENT_STATE_WAITING, 1)),
			press:    []string{"j"},
			then:     []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("quiet", "w1", 1), newTriageAgent("early", agentv1.AgentState_AGENT_STATE_WAITING, 1)},
			want:     []string{"early", "backend", "quiet"},
			hovered:  "early",
		},
		{
			name:     "the agent the cursor is on archived lands it on the next in triage",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"j"},
			then:     withoutThird(),
			want:     []string{"asker", "broken", "backend", "quiet"},
			hovered:  "broken",
		},
		{
			name:     "an answered agent leaves once the cursor moves off it",
			capacity: 3,
			rows:     buildTriage(),
			relisted: buildAnswered(),
			press:    []string{"j"},
			then:     buildAnswered(),
			want:     []string{"third", "broken", "backend", "asker", "quiet"},
			hovered:  "third",
		},
		{
			name:     "the agent the cursor is on keeps its place while its pane has the keys",
			capacity: 3,
			rows:     buildTriage(),
			blur:     true,
			relisted: buildAnswered(),
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
			hovered:  "asker",
		},
		{
			name:     "the agent the cursor is on keeps its place once it is answered",
			capacity: 3,
			rows:     buildTriage(),
			relisted: buildAnswered(),
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone holds while the panel gives up the keys",
			capacity: 3,
			rows:     withoutThird(),
			blur:     true,
			relisted: buildTriage(),
			want:     []string{"asker", "broken", "+1 more waiting for a slot", "backend", "third", "quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone settles once the panel leaves the agents list",
			capacity: 3,
			rows:     withoutThird(),
			relisted: buildTriage(),
			press:    []string{"tab", "shift+tab", "esc"},
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone settles once the cursor leaves it",
			capacity: 3,
			rows:     withoutThird(),
			relisted: buildTriage(),
			press:    []string{"j", "j"},
			want:     []string{"asker", "third", "broken", "backend", "quiet"},
			hovered:  "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			harness.press("t")
			if tt.blur {
				harness.send(tea.BlurMsg{})
			}
			if tt.relisted != nil {
				harness.watch(buildSnapshot(tt.relisted...))
			}
			harness.press(tt.press...)
			if tt.then != nil {
				harness.watch(buildSnapshot(tt.then...))
			}

			frame := harness.readFrame()
			if tt.want != nil {
				assert.Equal(t, tt.want, readTriageList(frame))
			}
			assert.Equal(t, tt.hovered, strings.TrimSpace(readHoveredRow(frame)))
		})
	}
}

func openTriage(t *testing.T, capacity int, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{triageRows: capacity})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(rows...))

	return harness
}

// buildTriage is a workflow with three agents waiting on a person and one that is not, broken the
// longest but in a tier below the others.
func buildTriage() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newAgentAt("quiet", "w1", 1),
		newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		newTriageAgent("broken", agentv1.AgentState_AGENT_STATE_ERRORED, 2),
		newTriageAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

func newTriageAgent(name string, state agentv1.AgentState, minute int) *agentv1.Agent {
	agent := newAgentAt(name, "w1", minute)
	agent.State = state
	agent.Alive = xptr.Ptr(true)
	agent.StateChangedAt = fmt.Sprintf("2026-09-20T13:%02d:00Z", minute)

	return agent
}

// readTriageList is the list's rows down to the record under it, with every run of spaces cut
// to one.
func readTriageList(frame string) []string {
	var rows []string
	for _, line := range splitLines(frame) {
		if strings.HasPrefix(line, "─") {
			break
		}

		for _, bar := range []string{"│ ", "┃ "} {
			if _, row, isRow := strings.Cut(line, bar); isRow {
				rows = append(rows, strings.Join(strings.Fields(row), " "))

				break
			}
		}
	}

	return rows
}

// buildFinished is a workflow with an agent asking and two implementers that have finished their
// turns, the earlier ahead of the later.
func buildFinished() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newAgentAt("quiet", "w1", 1),
		newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		newFinishedAgent("done", 5),
		newFinishedAgent("later", 6),
	}
}

func newFinishedAgent(name string, minute int) *agentv1.Agent {
	agent := newTriageAgent(name, agentv1.AgentState_AGENT_STATE_IDLE, minute)
	agent.Role = agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
	agent.PaneId = "%" + name

	return agent
}

// buildTriageFamily is a workflow with a parent whose reviewer is asking, ahead of another agent
// asking.
func buildTriageFamily() []proto.Message {
	reviewer := newTriageAgent("reviewer", agentv1.AgentState_AGENT_STATE_WAITING, 3)
	reviewer.ParentAgentId = xptr.Ptr("parent")

	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newAgentAt("quiet", "w1", 1),
		newTriageAgent("parent", agentv1.AgentState_AGENT_STATE_IDLE, 2),
		reviewer,
		newTriageAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

// buildAnswered is buildTriage once asker has been answered and gone back to work.
func buildAnswered() []proto.Message {
	rows := buildTriage()
	rows[2] = newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_THINKING, 3)

	return rows
}
