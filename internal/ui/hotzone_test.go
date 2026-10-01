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

// TestHotZoneRows covers which agents the zone draws and in what order.
func TestHotZoneRows(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		rows     []proto.Message
		want     []string
	}{
		{
			name:     "a zone with no rows is not drawn",
			capacity: 0,
			rows:     buildHot(),
			want:     []string{"backend", "third", "asker", "broken", "quiet"},
		},
		{
			name:     "a zone with nothing hot is not drawn",
			capacity: 2,
			rows:     []proto.Message{newLiveWorkflow("w1", "backend"), newHotAgent("quiet", agentv1.AgentState_AGENT_STATE_IDLE, 1)},
			want:     []string{"backend", "quiet"},
		},
		{
			name:     "an orchestrator is named for its workflow once",
			capacity: 2,
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				func() *agentv1.Agent {
					orchestrator := newOrchestrator()
					orchestrator.State = agentv1.AgentState_AGENT_STATE_WAITING

					return orchestrator
				}(),
			},
			want: []string{"hot 1/2", "backend asking", "backend"},
		},
		{
			name:     "the oldest hot agents take the slots and the rest wait in the tree",
			capacity: 2,
			rows:     buildHot(),
			want:     []string{"hot 2/2", "broken backend · errored", "asker backend · asking", "+1 more waiting for a slot", "backend", "third", "quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openHotZone(t, tt.capacity, tt.rows...)

			assert.Equal(t, tt.want, readHotList(harness.readFrame()))
		})
	}
}

// TestHotZoneFocus covers the hot zone key moving the keyboard into the zone and back out.
func TestHotZoneFocus(t *testing.T) {
	tests := []struct {
		name  string
		rows  []proto.Message
		press []string
		want  string
		note  string
	}{
		{
			name:  "with nothing hot the key says so and stays in the tree",
			rows:  []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("quiet", "w1", 1)},
			press: []string{"h"},
			want:  "quiet",
			note:  "nothing is hot",
		},
		{
			name:  "the key puts the cursor on the oldest hot agent",
			rows:  buildHot(),
			press: []string{"G", "h"},
			want:  "broken",
		},
		{
			name:  "the cursor stays inside the zone",
			rows:  buildHot(),
			press: []string{"h", "G"},
			want:  "asker",
		},
		{
			name:  "the key again goes back to where it was pressed",
			rows:  buildHot(),
			press: []string{"G", "h", "j", "h"},
			want:  "quiet",
		},
		{
			name:  "esc goes back to where it was pressed",
			rows:  buildHot(),
			press: []string{"G", "h", "esc"},
			want:  "quiet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openHotZone(t, 2, tt.rows...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.want, strings.TrimSpace(readHoveredRow(frame)))
			assert.Contains(t, frame, tt.note)
		})
	}
}

// TestHotZoneAdvance covers the cursor moving on once the agent it stood on no longer needs a
// person.
func TestHotZoneAdvance(t *testing.T) {
	answered := func(name string, state agentv1.AgentState) []proto.Message {
		rows := buildHot()
		for _, row := range rows {
			if agent, ok := row.(*agentv1.Agent); ok && agent.GetName() == name {
				agent.State = state
			}
		}

		return rows
	}

	t.Run("an answer moves the cursor to the next hot agent", func(t *testing.T) {
		harness := openHotZone(t, 2, buildHot()...)
		harness.press("h")

		harness.watch(buildSnapshot(answered("broken", agentv1.AgentState_AGENT_STATE_THINKING)...))

		assert.Equal(t, "asker", strings.TrimSpace(readHoveredRow(harness.readFrame())))
	})

	t.Run("an answer typed in the pane holds the cursor until the panel has the keyboard back", func(t *testing.T) {
		harness := openHotZone(t, 2, buildHot()...)
		harness.press("h")
		harness.send(tea.BlurMsg{})

		harness.watch(buildSnapshot(answered("broken", agentv1.AgentState_AGENT_STATE_THINKING)...))
		assert.Equal(t, "broken", strings.TrimSpace(readHoveredRow(harness.readFrame())))

		harness.send(tea.FocusMsg{})
		assert.Equal(t, "asker", strings.TrimSpace(readHoveredRow(harness.readFrame())))
	})

	t.Run("answering the last hot agent goes back to where the key was pressed", func(t *testing.T) {
		harness := openHotZone(t, 2,
			newLiveWorkflow("w1", "backend"),
			newAgentAt("quiet", "w1", 1),
			newHotAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		)
		harness.press("G", "h")

		harness.watch(buildSnapshot(
			newLiveWorkflow("w1", "backend"),
			newAgentAt("quiet", "w1", 1),
			newHotAgent("asker", agentv1.AgentState_AGENT_STATE_THINKING, 3),
		))

		frame := harness.readFrame()
		assert.Equal(t, "quiet", strings.TrimSpace(readHoveredRow(frame)))
		assert.Contains(t, frame, "hot zone clear")
		assert.NotContains(t, readHotList(frame), "hot 0/2")
	})
}

func openHotZone(t *testing.T, capacity int, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{hotZoneRows: capacity})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(rows...))

	return harness
}

// buildHot is a workflow with three agents waiting on a person and one that is not, broken the
// longest.
func buildHot() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newAgentAt("quiet", "w1", 1),
		newHotAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		newHotAgent("broken", agentv1.AgentState_AGENT_STATE_ERRORED, 2),
		newHotAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

func newHotAgent(name string, state agentv1.AgentState, minute int) *agentv1.Agent {
	agent := newAgentAt(name, "w1", minute)
	agent.State = state
	agent.Alive = xptr.Ptr(true)
	agent.StateChangedAt = fmt.Sprintf("2026-09-20T13:%02d:00Z", minute)

	return agent
}

// readHotList is the list's rows down to the record under it, notes and all, with every run of
// spaces cut to one.
func readHotList(frame string) []string {
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
