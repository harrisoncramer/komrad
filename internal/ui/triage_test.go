package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

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
