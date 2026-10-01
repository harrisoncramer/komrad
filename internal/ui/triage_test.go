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
			want: []string{"triage 1/2", "backend asking", "backend"},
		},
		{
			name:     "the oldest agents in triage take the slots and the rest wait in the tree",
			capacity: 2,
			rows:     buildTriage(),
			want:     []string{"triage 2/2", "broken backend · errored", "asker backend · asking", "+1 more waiting for a slot", "backend", "third", "quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			assert.Equal(t, tt.want, readTriageList(harness.readFrame()))
		})
	}
}

// TestTriageFocus covers the triage key moving the keyboard into the zone and back out.
func TestTriageFocus(t *testing.T) {
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
			name:  "the key puts the cursor on the oldest agent in triage",
			rows:  buildTriage(),
			press: []string{"G", "t"},
			want:  "broken",
		},
		{
			name:  "the cursor stays inside the zone",
			rows:  buildTriage(),
			press: []string{"t", "G"},
			want:  "asker",
		},
		{
			name:  "the key again goes back to where it was pressed",
			rows:  buildTriage(),
			press: []string{"G", "t", "j", "t"},
			want:  "quiet",
		},
		{
			name:  "esc goes back to where it was pressed",
			rows:  buildTriage(),
			press: []string{"G", "t", "esc"},
			want:  "quiet",
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

// TestTriageAdvance covers the cursor moving on once the agent it stood on no longer needs a
// person.
func TestTriageAdvance(t *testing.T) {
	answered := func(name string, state agentv1.AgentState) []proto.Message {
		rows := buildTriage()
		for _, row := range rows {
			if agent, ok := row.(*agentv1.Agent); ok && agent.GetName() == name {
				agent.State = state
			}
		}

		return rows
	}

	t.Run("an answer moves the cursor to the next agent in triage", func(t *testing.T) {
		harness := openTriage(t, 2, buildTriage()...)
		harness.press("t")

		harness.watch(buildSnapshot(answered("broken", agentv1.AgentState_AGENT_STATE_THINKING)...))

		assert.Equal(t, "asker", strings.TrimSpace(readHoveredRow(harness.readFrame())))
	})

	t.Run("an answer typed in the pane holds the cursor until the panel has the keyboard back", func(t *testing.T) {
		harness := openTriage(t, 2, buildTriage()...)
		harness.press("t")
		harness.send(tea.BlurMsg{})

		harness.watch(buildSnapshot(answered("broken", agentv1.AgentState_AGENT_STATE_THINKING)...))
		assert.Equal(t, "broken", strings.TrimSpace(readHoveredRow(harness.readFrame())))

		harness.send(tea.FocusMsg{})
		assert.Equal(t, "asker", strings.TrimSpace(readHoveredRow(harness.readFrame())))
	})

	t.Run("answering the last agent in triage goes back to where the key was pressed", func(t *testing.T) {
		harness := openTriage(t, 2,
			newLiveWorkflow("w1", "backend"),
			newAgentAt("quiet", "w1", 1),
			newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		)
		harness.press("G", "t")

		harness.watch(buildSnapshot(
			newLiveWorkflow("w1", "backend"),
			newAgentAt("quiet", "w1", 1),
			newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_THINKING, 3),
		))

		frame := harness.readFrame()
		assert.Equal(t, "quiet", strings.TrimSpace(readHoveredRow(frame)))
		assert.Contains(t, frame, "triage clear")
		assert.NotContains(t, readTriageList(frame), "triage 0/2")
	})
}

func openTriage(t *testing.T, capacity int, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{triageRows: capacity})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(rows...))

	return harness
}

// buildTriage is a workflow with three agents waiting on a person and one that is not, broken the
// longest.
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

// readTriageList is the list's rows down to the record under it, notes and all, with every run of
// spaces cut to one.
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
