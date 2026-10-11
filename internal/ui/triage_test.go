package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

// TestTriageRows covers which agents the zone renders and in what order.
func TestTriageRows(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		rows     []proto.Message
		// press are the keys pressed before the list is read.
		press []string
		want  []string
	}{
		{
			name:     "a zone with no rows is not rendered",
			capacity: 0,
			rows:     buildTriage(),
			want:     []string{"backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "a zone with nothing in triage says so",
			capacity: 2,
			rows:     []proto.Message{newLiveWorkflow("w1", "backend"), newTriageAgent("└─ └─ quiet", agentv1.AgentState_AGENT_STATE_IDLE, 1)},
			want:     []string{"nothing to triage", "backend", "└─ └─ └─ quiet"},
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
			name:     "a writer is followed by its workflow",
			capacity: 2,
			rows:     []proto.Message{newLiveWorkflow("w1", "backend"), newFinishedAgent("done", 5)},
			want:     []string{"done backend", "backend", "└─ done"},
		},
		{
			name:     "a writer in triage brings its reviewers with it",
			capacity: 3,
			rows: append(withTasks([]proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgentAt("quiet", "w1", 1),
				newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
			}),
				newReviewer("reviewer", "reviewer", "asker"),
				newReviewer("tester", "tester", "asker"),
			),
			want: []string{"asker backend", "└ tester", "└ reviewer", "backend", "├─ task-asker", "│ ├─ asker", "│ ├─ tester", "│ └─ reviewer", "└─ task-quiet", "└─ quiet"},
		},
		{
			name:     "a reviewer in triage brings its writer with it",
			capacity: 2,
			rows:     buildTriageFamily()[:5],
			want:     []string{"author backend", "└ reviewer", "backend", "├─ task-author", "│ ├─ author", "│ └─ reviewer", "└─ quiet"},
		},
		{
			name:     "the most urgent tier takes the slots and the rest wait in the tree",
			capacity: 2,
			rows:     buildTriage(),
			want:     []string{"asker", "third", "+1 more waiting for a slot", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "within a tier the agent waiting longest comes first",
			capacity: 3,
			rows:     buildTriage(),
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "a family with a reviewer asking takes one slot",
			capacity: 2,
			rows:     append(withTasks(buildTriage()[:3]), buildTriageFamily()[2:5]...),
			want:     []string{"asker backend", "author backend", "└ reviewer", "backend", "├─ task-asker", "│ └─ asker", "├─ task-author", "│ ├─ author", "│ └─ reviewer", "└─ task-quiet", "└─ quiet"},
		},
		{
			name:     "three families come whole with their reviewers and a fourth waits for a slot",
			capacity: 3,
			rows: append(withTasks(buildTriage()),
				newReviewer("tester", "tester", "asker"),
				buildTriageFamily()[2],
				buildTriageFamily()[3],
				buildTriageFamily()[4],
				newReviewer("third-reviewer", "third-reviewer", "third"),
			),
			want: []string{"asker backend", "└ tester", "author backend", "└ reviewer", "third backend", "└ third-reviewer", "+1 more waiting for a slot", "backend", "├─ task-third", "│ ├─ third", "│ └─ third-reviewer", "├─ task-asker", "│ ├─ asker", "│ └─ tester", "├─ task-broken", "│ └─ broken"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readTriageList(harness.readFrame()))
		})
	}
}

// TestTriageHeight covers the zone keeping a row for every slot however many families it holds.
func TestTriageHeight(t *testing.T) {
	tests := []struct {
		name string
		rows []proto.Message
	}{
		{name: "full", rows: append(buildTriage(), newTriageAgent("fourth", agentv1.AgentState_AGENT_STATE_WAITING, 5))},
		{name: "part full", rows: buildTriage()[:3]},
	}

	want := findTreeLine(openTriage(t, 4, buildTriage()[:2]...).readFrame())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, 4, tt.rows...)

			assert.Equal(t, want, findTreeLine(harness.readFrame()))
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
			rows:  []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("└─ quiet", "w1", 1)},
			press: []string{"t"},
			want:  "└─ └─ quiet",
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

// TestTriageSnooze covers taking a family out of the zone until what it waits on changes, as the
// zone stands once the cursor has left it.
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
		// want is the list as rendered, unchecked when nil.
		want []string
	}{
		{
			name:     "a family whose head was archived comes back unsnoozed",
			capacity: 3,
			rows:     buildTriageFamily(),
			press:    []string{"t", "space", "z"},
			relisted: []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("quiet", "w1", 1), buildTriageFamily()[5]},
			then:     buildTriageFamily(),
			want:     []string{"author backend", "└ reviewer", "third", "backend", "├─ third", "├─ task-author", "│ ├─ author", "│ └─ reviewer", "└─ quiet"},
		},
		{
			name:     "a family hidden and shown again keeps its snooze",
			capacity: 3,
			rows:     withTasks(buildTriage()),
			press:    []string{"t", "space", "z"},
			relisted: hideTaskOf(withTasks(buildTriage()), "asker"),
			then:     withTasks(buildTriage()),
			want:     []string{"third backend", "broken backend", "backend", "├─ task-third", "│ └─ third", "├─ task-asker", "│ └─ asker", "├─ task-broken", "│ └─ broken", "└─ task-quiet", "└─ quiet"},
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
			want: []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
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
			want: []string{"third", "asker", "broken", "backend", "├─ third", "├─ broken", "├─ quiet", "└─ asker"},
		},
		{
			name:     "opening an agent that asks leaves it where it is",
			capacity: 3,
			rows:     buildFinished(),
			press:    []string{"t", "enter"},
			want:     []string{"asker", "done backend", "later backend", "backend", "├─ later", "├─ done", "├─ asker", "└─ quiet"},
		},
		{
			name:     "waking a snoozed family from the tree puts it back in the zone",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z", "G", "k", "k", "space", "z"},
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "waking a snoozed task from its row in the tree puts it back in the zone",
			capacity: 3,
			rows:     withTasks(buildTriage()),
			press:    []string{"t", "space", "z", "G", "k", "k", "k", "k", "k", "space", "z"},
			want:     []string{"asker backend", "third backend", "broken backend", "backend", "├─ task-third", "│ └─ third", "├─ task-asker", "│ └─ asker", "├─ task-broken", "│ └─ broken", "└─ task-quiet", "└─ quiet"},
		},
		{
			name:     "waking a snoozed orchestrator from its workflow puts it back in the zone",
			capacity: 3,
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgentAt("quiet", "w1", 1),
				func() *agentv1.Agent {
					orchestrator := newOrchestrator()
					orchestrator.State = agentv1.AgentState_AGENT_STATE_WAITING

					return orchestrator
				}(),
			},
			press: []string{"t", "space", "z", "k", "space", "z"},
			want:  []string{"backend", "backend", "└─ quiet"},
		},
		{
			name:     "a snoozed family gives up its slot",
			capacity: 2,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "a snooze outlasts a listing that changes nothing",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: buildTriage(),
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "opening a finished agent snoozes it",
			capacity: 3,
			rows:     buildFinished(),
			press:    []string{"t", "j", "enter"},
			want:     []string{"asker", "later backend", "backend", "├─ later", "├─ done", "├─ asker", "└─ quiet"},
		},
		{
			name:     "a snoozed family leaves the zone and stays in the tree",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"t", "space", "z"},
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
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
		// note is what the messages region says, unchecked when empty.
		note string
	}{
		{
			name:     "an agent that needs triage while the zone is read waits for a slot",
			capacity: 3,
			rows:     withoutThird(),
			relisted: buildTriage(),
			want:     []string{"asker", "broken", "+1 more waiting for a slot", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
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
			want:    []string{"asker", "+1 more waiting for a slot", "backend", "├─ third ⠋", "├─ asker", "├─ broken", "└─ quiet"},
			hovered: "asker",
		},
		{
			name:     "a family snoozed while the zone is read leaves it and the cursor moves on to the next",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"space", "z"},
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "third",
		},
		{
			name:     "a family snoozed below the top takes the cursor to the one above",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"j", "j", "space", "z"},
			want:     []string{"asker", "third", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "third",
		},
		{
			name:     "a family waiting for a slot comes in once snoozes make room for it",
			capacity: 3,
			rows:     append(buildTriage(), newTriageAgent("fourth", agentv1.AgentState_AGENT_STATE_WAITING, 5)),
			press:    []string{"space", "z", "space", "z", "space", "z"},
			want:     []string{"broken", "backend", "├─ fourth", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "broken",
		},
		{
			name:     "the last family snoozed takes the cursor to the first agent in the list and says so",
			capacity: 3,
			rows:     append(buildTriage()[:3], newTriageAgent("├─ newest", agentv1.AgentState_AGENT_STATE_IDLE, 9)),
			press:    []string{"space", "z"},
			want:     []string{"nothing to triage", "backend", "├─ ├─ newest", "├─ asker", "└─ quiet"},
			hovered:  "├─ ├─ newest",
			note:     "nothing left to triage",
		},
		{
			name:     "opening a finished agent in the zone moves the cursor to its tree row",
			capacity: 3,
			rows:     buildFinished(),
			press:    []string{"j", "enter"},
			want:     []string{"asker", "later backend", "backend", "├─ later", "├─ done", "├─ asker", "└─ quiet"},
			hovered:  "├─ done",
		},
		{
			name:     "opening a finished agent the filter keeps out of the tree leaves it in the zone",
			capacity: 3,
			rows:     buildFinished(),
			press:    []string{"/", "tab", "q", "u", "i", "enter", "t", "j", "enter"},
			want:     []string{"asker", "done backend", "later backend", "backend", "└─ quiet"},
			hovered:  "done backend",
		},
		{
			name:     "the last agent in triage archived lands the cursor in the tree",
			capacity: 3,
			rows:     buildTriage()[:3],
			relisted: buildTriage()[:2],
			hovered:  "└─ quiet",
		},
		{
			name:     "a family archived from the bottom of the zone lands the cursor on the one above",
			capacity: 3,
			rows:     append(buildTriageFamily()[:5], newTriageAgent("early", agentv1.AgentState_AGENT_STATE_WAITING, 1)),
			press:    []string{"j"},
			then:     []proto.Message{newLiveWorkflow("w1", "backend"), newAgentAt("quiet", "w1", 1), newTriageAgent("early", agentv1.AgentState_AGENT_STATE_WAITING, 1)},
			want:     []string{"early", "backend", "├─ quiet", "└─ early"},
			hovered:  "early",
		},
		{
			name:     "the agent the cursor is on archived lands it on the next in triage",
			capacity: 3,
			rows:     buildTriage(),
			press:    []string{"j"},
			then:     withoutThird(),
			want:     []string{"asker", "broken", "backend", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "broken",
		},
		{
			name:     "an answered agent leaves once the cursor moves off it",
			capacity: 3,
			rows:     buildTriage(),
			relisted: buildAnswered(),
			press:    []string{"j"},
			then:     buildAnswered(),
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker ⠋", "├─ broken", "└─ quiet"},
			hovered:  "third",
		},
		{
			name:     "the agent the cursor is on keeps its place while its pane has the keys",
			capacity: 3,
			rows:     buildTriage(),
			blur:     true,
			relisted: buildAnswered(),
			want:     []string{"asker ⠋", "third", "broken", "backend", "├─ third", "├─ asker ⠋", "├─ broken", "└─ quiet"},
			hovered:  "asker ⠋",
		},
		{
			name:     "the agent the cursor is on keeps its place once it is answered",
			capacity: 3,
			rows:     buildTriage(),
			relisted: buildAnswered(),
			want:     []string{"asker ⠋", "third", "broken", "backend", "├─ third", "├─ asker ⠋", "├─ broken", "└─ quiet"},
			hovered:  "asker ⠋",
		},
		{
			name:     "the zone holds while the panel gives up the keys",
			capacity: 3,
			rows:     withoutThird(),
			blur:     true,
			relisted: buildTriage(),
			want:     []string{"asker", "broken", "+1 more waiting for a slot", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone settles once the panel leaves the agents list",
			capacity: 3,
			rows:     withoutThird(),
			relisted: buildTriage(),
			press:    []string{"tab", "shift+tab", "esc"},
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone settles once the cursor leaves it",
			capacity: 3,
			rows:     withoutThird(),
			relisted: buildTriage(),
			press:    []string{"j", "j"},
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
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
			assert.Contains(t, frame, tt.note)
		})
	}
}

// TestTriageCopies covers an agent in triage rendered twice, once in the zone and once in the tree.
func TestTriageCopies(t *testing.T) {
	bar := paint.BackgroundSequence(paint.Panel.Background(paint.ShadeSelection))

	t.Run("marking the zone's copy marks the tree's too", func(t *testing.T) {
		harness := openTriage(t, 3, buildTriage()...)

		harness.press("t", "v")

		marked := xslice.Filter(findRenderedRows(t, harness, "asker"), func(line string) bool {
			return strings.Contains(line, bar)
		})
		assert.Len(t, marked, 2)
	})

	t.Run("an action over both copies takes the agent once", func(t *testing.T) {
		harness := openTriage(t, 3, withTasks(buildTriage())...)

		harness.press("t", "v", "space", "a")

		frame := harness.readFrame()
		assert.Contains(t, frame, `agent "asker"`)
		assert.NotContains(t, frame, "2 agents")
	})

	t.Run("a zone row acts through the agent its task's row wears", func(t *testing.T) {
		writer := newTriageAgent("writer", agentv1.AgentState_AGENT_STATE_WAITING, 3)
		task := newTaskOf(writer)
		sweeper := newAgentAt("sweeper", "w1", 9)
		sweeper.PaneId = "%5"
		sweeper.Alive = xptr.Ptr(true)
		sweeper.TaskId = writer.TaskId
		harness := openTriage(t, 3, newLiveWorkflow("w1", "backend"), writer, task, sweeper)

		harness.press("t", "space", "x")

		assert.Contains(t, harness.readFrame(), "agents › sweeper › status checks")
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
	return xslice.Map(readListLines(frame), func(line listLine) string {
		return strings.Join(strings.Fields(line.row), " ")
	})
}

// buildFinished is a workflow with an agent asking and two writers that have finished their
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
	agent.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	agent.PaneId = "%" + name

	return agent
}

// buildTriageFamily is a workflow with a writer whose reviewer is asking, ahead of another agent
// asking.
func buildTriageFamily() []proto.Message {
	author := newTriageAgent("author", agentv1.AgentState_AGENT_STATE_IDLE, 2)
	authorTask := newTaskOf(author)
	reviewer := newTriageAgent("reviewer", agentv1.AgentState_AGENT_STATE_WAITING, 3)
	reviewer.TaskId = author.TaskId
	reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newAgentAt("quiet", "w1", 1),
		author,
		reviewer,
		authorTask,
		newTriageAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

// buildAnswered is buildTriage once asker has been answered and gone back to work.
func buildAnswered() []proto.Message {
	rows := buildTriage()
	rows[2] = newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_THINKING, 3)

	return rows
}

// findTreeLine is the line of the frame the tree's workflow is rendered on.
func findTreeLine(frame string) int {
	return slices.IndexFunc(splitLines(frame), func(line string) bool { return strings.Contains(line, "│ backend") })
}
