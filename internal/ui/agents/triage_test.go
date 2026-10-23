package agents_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
			rows:     paneltest.BuildTriage(),
			want:     []string{"backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "a zone with nothing in triage says so",
			capacity: 2,
			rows:     []proto.Message{paneltest.Workflow("w1", "backend"), paneltest.TriageAgent("└─ └─ quiet", agentv1.AgentState_AGENT_STATE_IDLE, 1)},
			want:     []string{"nothing to triage", "backend", "└─ └─ └─ quiet"},
		},
		{
			name:     "an orchestrator is named for its workflow",
			capacity: 2,
			rows: []proto.Message{
				paneltest.Workflow("w1", "backend"),
				func() *agentv1.Agent {
					orchestrator := paneltest.Orchestrator()
					orchestrator.State = agentv1.AgentState_AGENT_STATE_WAITING

					return orchestrator
				}(),
			},
			want: []string{"backend", "backend"},
		},
		{
			name:     "a writer is followed by its workflow",
			capacity: 2,
			rows:     []proto.Message{paneltest.Workflow("w1", "backend"), newFinishedAgent("done", 5)},
			want:     []string{"done backend", "backend", "└─ done"},
		},
		{
			name:     "a writer in triage brings its reviewers with it",
			capacity: 3,
			rows: append(withTasks([]proto.Message{
				paneltest.Workflow("w1", "backend"),
				paneltest.AgentAt("quiet", "w1", 1),
				paneltest.TriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
			}),
				paneltest.Reviewer("reviewer", "reviewer", "asker"),
				paneltest.Reviewer("second-reviewer", "second-reviewer", "asker"),
			),
			want: []string{"asker backend", "└ " + paneltest.ReviewerMark + "second-reviewer", "└ " + paneltest.ReviewerMark + "reviewer", "backend", "├─ task-asker", "│ ├─ " + paneltest.ReviewerMark + "second-reviewer", "│ └─ " + paneltest.ReviewerMark + "reviewer", "└─ task-quiet"},
		},
		{
			name:     "a reviewer in triage brings its writer with it",
			capacity: 2,
			rows:     buildTriageTask()[:5],
			want:     []string{"writer backend", "└ " + paneltest.ReviewerMark + "reviewer", "backend", "├─ task-writer", "│ └─ " + paneltest.ReviewerMark + "reviewer", "└─ quiet"},
		},
		{
			name:     "the most urgent tier takes the slots and the rest wait in the tree",
			capacity: 2,
			rows:     paneltest.BuildTriage(),
			want:     []string{"asker", "third", "+1 more waiting for a slot", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "within a tier the agent waiting longest comes first",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "a task with a reviewer asking takes one slot",
			capacity: 2,
			rows:     append(withTasks(paneltest.BuildTriage()[:3]), buildTriageTask()[2:5]...),
			want:     []string{"asker backend", "writer backend", "└ " + paneltest.ReviewerMark + "reviewer", "backend", "├─ task-asker", "├─ task-writer", "│ └─ " + paneltest.ReviewerMark + "reviewer", "└─ task-quiet"},
		},
		{
			name:     "three tasks come whole with their reviewers and a fourth waits for a slot",
			capacity: 3,
			rows: append(withTasks(paneltest.BuildTriage()),
				paneltest.Reviewer("second-reviewer", "second-reviewer", "asker"),
				buildTriageTask()[2],
				buildTriageTask()[3],
				buildTriageTask()[4],
				paneltest.Reviewer("third-reviewer", "third-reviewer", "third"),
			),
			want: []string{"asker backend", "└ " + paneltest.ReviewerMark + "second-reviewer", "writer backend", "└ " + paneltest.ReviewerMark + "reviewer", "third backend", "└ " + paneltest.ReviewerMark + "third-reviewer", "+1 more waiting for a slot", "backend", "├─ task-third", "│ └─ " + paneltest.ReviewerMark + "third-reviewer", "├─ task-asker", "│ └─ " + paneltest.ReviewerMark + "second-reviewer", "├─ task-writer", "│ └─ " + paneltest.ReviewerMark + "reviewer", "├─ task-broken", "└─ task-quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			harness.Press(tt.press...)

			assert.Equal(t, tt.want, readTriageList(harness.Frame()))
		})
	}
}

// TestTriageHeight covers the zone keeping a row for every slot however many agents it holds.
func TestTriageHeight(t *testing.T) {
	tests := []struct {
		name string
		rows []proto.Message
	}{
		{name: "full", rows: append(paneltest.BuildTriage(), paneltest.TriageAgent("fourth", agentv1.AgentState_AGENT_STATE_WAITING, 5))},
		{name: "part full", rows: paneltest.BuildTriage()[:3]},
	}

	want := findTreeLine(openTriage(t, 4, paneltest.BuildTriage()[:2]...).Frame())
	require.GreaterOrEqual(t, want, 0)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, 4, tt.rows...)

			assert.Equal(t, want, findTreeLine(harness.Frame()))
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
			rows:  []proto.Message{paneltest.Workflow("w1", "backend"), paneltest.AgentAt("└─ quiet", "w1", 1)},
			press: []string{"t"},
			want:  "└─ └─ quiet",
			note:  "nothing to triage",
		},
		{
			name:  "the key puts the cursor on the first agent in triage",
			rows:  paneltest.BuildTriage(),
			press: []string{"G", "t"},
			want:  "asker",
		},
		{
			name:  "the cursor moves on out of the zone into the tree",
			rows:  paneltest.BuildTriage(),
			press: []string{"t", "j", "j"},
			want:  "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, 2, tt.rows...)

			harness.Press(tt.press...)

			frame := harness.Frame()
			assert.Equal(t, tt.want, strings.TrimSpace(paneltest.HoveredRow(frame)))
			assert.Contains(t, frame, tt.note)
		})
	}
}

// TestTriageSnooze covers taking a task out of the zone until what it waits on changes, as the
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
			name:     "a task whose writer was archived comes back unsnoozed",
			capacity: 3,
			rows:     buildTriageTask(),
			press:    []string{"t", "space", "z"},
			relisted: []proto.Message{paneltest.Workflow("w1", "backend"), paneltest.AgentAt("quiet", "w1", 1), buildTriageTask()[5]},
			then:     buildTriageTask(),
			want:     []string{"writer backend", "└ " + paneltest.ReviewerMark + "reviewer", "third", "backend", "├─ third", "├─ task-writer", "│ └─ " + paneltest.ReviewerMark + "reviewer", "└─ quiet"},
		},
		{
			name:     "a task hidden and shown again keeps its snooze",
			capacity: 3,
			rows:     withTasks(paneltest.BuildTriage()),
			press:    []string{"t", "space", "z"},
			relisted: hideTaskOf(withTasks(paneltest.BuildTriage()), "asker"),
			then:     withTasks(paneltest.BuildTriage()),
			want:     []string{"third backend", "broken backend", "backend", "├─ task-third", "├─ task-asker", "├─ task-broken", "└─ task-quiet"},
		},
		{
			name:     "an agent that asks again ends its snooze",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: func() []proto.Message {
				rows := paneltest.BuildTriage()
				rows[2].(*agentv1.Agent).StateChangedAt = timestamppb.New(time.Date(2026, 9, 20, 13, 6, 0, 0, time.UTC))

				return rows
			}(),
			want: []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "an agent that moves tier ends its snooze",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: func() []proto.Message {
				rows := paneltest.BuildTriage()
				rows[2] = paneltest.TriageAgent("asker", agentv1.AgentState_AGENT_STATE_ERRORED, 1)

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
			name:     "waking a snoozed agent from the tree puts it back in the zone",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"t", "space", "z", "G", "k", "k", "space", "z"},
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "waking a snoozed task from its row in the tree puts it back in the zone",
			capacity: 3,
			rows:     withTasks(paneltest.BuildTriage()),
			press:    []string{"t", "space", "z", "G", "k", "k", "space", "z"},
			want:     []string{"asker backend", "third backend", "broken backend", "backend", "├─ task-third", "├─ task-asker", "├─ task-broken", "└─ task-quiet"},
		},
		{
			name:     "waking a snoozed orchestrator from its workflow puts it back in the zone",
			capacity: 3,
			rows: []proto.Message{
				paneltest.Workflow("w1", "backend"),
				paneltest.AgentAt("quiet", "w1", 1),
				func() *agentv1.Agent {
					orchestrator := paneltest.Orchestrator()
					orchestrator.State = agentv1.AgentState_AGENT_STATE_WAITING

					return orchestrator
				}(),
			},
			press: []string{"t", "space", "z", "k", "space", "z"},
			want:  []string{"backend", "backend", "└─ quiet"},
		},
		{
			name:     "a snoozed agent gives up its slot",
			capacity: 2,
			rows:     paneltest.BuildTriage(),
			press:    []string{"t", "space", "z"},
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
		{
			name:     "a snooze outlasts a listing that changes nothing",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"t", "space", "z"},
			relisted: paneltest.BuildTriage(),
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
			name:     "a snoozed agent leaves the zone and stays in the tree",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"t", "space", "z"},
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			harness.Press(tt.press...)
			if tt.relisted != nil {
				harness.Watch(paneltest.Snapshot(tt.relisted...))
			}
			if tt.then != nil {
				harness.Watch(paneltest.Snapshot(tt.then...))
			}
			harness.Press("G")

			assert.Equal(t, tt.want, readTriageList(harness.Frame()))
		})
	}
}

// TestTriageFreeze covers the zone holding still while a person reads it and settling once they
// leave.
func TestTriageFreeze(t *testing.T) {
	withoutThird := func() []proto.Message { return paneltest.BuildTriage()[:4] }
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
			relisted: paneltest.BuildTriage(),
			want:     []string{"asker", "broken", "+1 more waiting for a slot", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "asker",
		},
		{
			name:     "a slot freed while the zone is read stays empty",
			capacity: 2,
			rows:     paneltest.BuildTriage(),
			relisted: func() []proto.Message {
				rows := paneltest.BuildTriage()
				rows[4] = paneltest.TriageAgent("third", agentv1.AgentState_AGENT_STATE_THINKING, 4)

				return rows
			}(),
			want:    []string{"asker", "+1 more waiting for a slot", "backend", "├─ third ⠋", "├─ asker", "├─ broken", "└─ quiet"},
			hovered: "asker",
		},
		{
			name:     "an agent snoozed while the zone is read leaves it and the cursor moves on to the next",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"space", "z"},
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "third",
		},
		{
			name:     "an agent snoozed below the top takes the cursor to the one above",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"j", "j", "space", "z"},
			want:     []string{"asker", "third", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "third",
		},
		{
			name:     "an agent waiting for a slot comes in once snoozes make room for it",
			capacity: 3,
			rows:     append(paneltest.BuildTriage(), paneltest.TriageAgent("fourth", agentv1.AgentState_AGENT_STATE_WAITING, 5)),
			press:    []string{"space", "z", "space", "z", "space", "z"},
			want:     []string{"broken", "backend", "├─ fourth", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "broken",
		},
		{
			name:     "the last agent snoozed takes the cursor to the first agent in the list and says so",
			capacity: 3,
			rows:     append(paneltest.BuildTriage()[:3], paneltest.TriageAgent("├─ newest", agentv1.AgentState_AGENT_STATE_IDLE, 9)),
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
			rows:     paneltest.BuildTriage()[:3],
			relisted: paneltest.BuildTriage()[:2],
			hovered:  "└─ quiet",
		},
		{
			name:     "a task archived from the bottom of the zone lands the cursor on the one above",
			capacity: 3,
			rows:     append(buildTriageTask()[:5], paneltest.TriageAgent("early", agentv1.AgentState_AGENT_STATE_WAITING, 1)),
			press:    []string{"j"},
			then:     []proto.Message{paneltest.Workflow("w1", "backend"), paneltest.AgentAt("quiet", "w1", 1), paneltest.TriageAgent("early", agentv1.AgentState_AGENT_STATE_WAITING, 1)},
			want:     []string{"early", "backend", "├─ quiet", "└─ early"},
			hovered:  "early",
		},
		{
			name:     "the agent the cursor is on archived lands it on the next in triage",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			press:    []string{"j"},
			then:     withoutThird(),
			want:     []string{"asker", "broken", "backend", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "broken",
		},
		{
			name:     "an answered agent leaves once the cursor moves off it",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			relisted: buildAnswered(),
			press:    []string{"j"},
			then:     buildAnswered(),
			want:     []string{"third", "broken", "backend", "├─ third", "├─ asker ⠋", "├─ broken", "└─ quiet"},
			hovered:  "third",
		},
		{
			name:     "the agent the cursor is on keeps its place while its pane has the keys",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			blur:     true,
			relisted: buildAnswered(),
			want:     []string{"asker ⠋", "third", "broken", "backend", "├─ third", "├─ asker ⠋", "├─ broken", "└─ quiet"},
			hovered:  "asker ⠋",
		},
		{
			name:     "the agent the cursor is on keeps its place once it is answered",
			capacity: 3,
			rows:     paneltest.BuildTriage(),
			relisted: buildAnswered(),
			want:     []string{"asker ⠋", "third", "broken", "backend", "├─ third", "├─ asker ⠋", "├─ broken", "└─ quiet"},
			hovered:  "asker ⠋",
		},
		{
			name:     "the zone holds while the panel gives up the keys",
			capacity: 3,
			rows:     withoutThird(),
			blur:     true,
			relisted: paneltest.BuildTriage(),
			want:     []string{"asker", "broken", "+1 more waiting for a slot", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone settles once the panel leaves the agents list",
			capacity: 3,
			rows:     withoutThird(),
			relisted: paneltest.BuildTriage(),
			press:    []string{"tab", "shift+tab", "esc"},
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "asker",
		},
		{
			name:     "the zone settles once the cursor leaves it",
			capacity: 3,
			rows:     withoutThird(),
			relisted: paneltest.BuildTriage(),
			press:    []string{"j", "j"},
			want:     []string{"asker", "third", "broken", "backend", "├─ third", "├─ asker", "├─ broken", "└─ quiet"},
			hovered:  "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTriage(t, tt.capacity, tt.rows...)

			harness.Press("t")
			if tt.blur {
				harness.Send(tea.BlurMsg{})
			}
			if tt.relisted != nil {
				harness.Watch(paneltest.Snapshot(tt.relisted...))
			}
			harness.Press(tt.press...)
			if tt.then != nil {
				harness.Watch(paneltest.Snapshot(tt.then...))
			}

			frame := harness.Frame()
			if tt.want != nil {
				assert.Equal(t, tt.want, readTriageList(frame))
			}
			assert.Equal(t, tt.hovered, strings.TrimSpace(paneltest.HoveredRow(frame)))
			assert.Contains(t, frame, tt.note)
		})
	}
}

// TestTriageCopies covers an agent in triage rendered twice, once in the zone and once in the tree.
func TestTriageCopies(t *testing.T) {
	bar := paint.BackgroundSequence(paint.Panel.Background(paint.ShadeSelection))

	t.Run("marking the zone's copy marks the tree's too", func(t *testing.T) {
		harness := openTriage(t, 3, paneltest.BuildTriage()...)

		harness.Press("t", "v")

		marked := xslice.Filter(findRenderedRows(t, harness, "asker"), func(line string) bool {
			return strings.Contains(line, bar)
		})
		assert.Len(t, marked, 2)
	})

	t.Run("an action over both copies takes the agent once", func(t *testing.T) {
		harness := openTriage(t, 3, withTasks(paneltest.BuildTriage())...)

		harness.Press("t", "v", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, frame, `agent "asker"`)
		assert.NotContains(t, frame, "2 agents")
	})

	t.Run("a zone row acts through its task's writer", func(t *testing.T) {
		writer := paneltest.TriageAgent("writer", agentv1.AgentState_AGENT_STATE_WAITING, 3)
		task := paneltest.TaskOf(writer)
		sweeper := paneltest.AgentAt("sweeper", "w1", 9)
		sweeper.PaneId = "%5"
		sweeper.Alive = xptr.Ptr(true)
		sweeper.TaskId = writer.TaskId
		sweeper.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
		harness := openTriage(t, 3, paneltest.Workflow("w1", "backend"), writer, task, sweeper)

		harness.Press("t", "space", "x")

		assert.Contains(t, harness.Frame(), "agents › writer › status checks")
	})
}

func openTriage(t *testing.T, capacity int, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{TriageRows: capacity})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(rows...))

	return harness
}

// readTriageList is the list's rows down to the record under it, with every run of spaces cut
// to one.
func readTriageList(frame string) []string {
	return xslice.Map(paneltest.ListLines(frame), func(line paneltest.ListLine) string {
		return strings.Join(strings.Fields(line.Row), " ")
	})
}

// buildFinished is a workflow with an agent asking and two writers that have finished their
// turns, the earlier ahead of the later.
func buildFinished() []proto.Message {
	return []proto.Message{
		paneltest.Workflow("w1", "backend"),
		paneltest.AgentAt("quiet", "w1", 1),
		paneltest.TriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		newFinishedAgent("done", 5),
		newFinishedAgent("later", 6),
	}
}

func newFinishedAgent(name string, minute int) *agentv1.Agent {
	agent := paneltest.TriageAgent(name, agentv1.AgentState_AGENT_STATE_IDLE, minute)
	agent.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	agent.PaneId = "%" + name

	return agent
}

// buildTriageTask is a workflow with a writer whose reviewer is asking, ahead of another agent
// asking.
func buildTriageTask() []proto.Message {
	writer := paneltest.TriageAgent("writer", agentv1.AgentState_AGENT_STATE_IDLE, 2)
	writerTask := paneltest.TaskOf(writer)
	reviewer := paneltest.TriageAgent("reviewer", agentv1.AgentState_AGENT_STATE_WAITING, 3)
	reviewer.TaskId = writer.TaskId
	reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

	return []proto.Message{
		paneltest.Workflow("w1", "backend"),
		paneltest.AgentAt("quiet", "w1", 1),
		writer,
		reviewer,
		writerTask,
		paneltest.TriageAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

// buildAnswered is paneltest.BuildTriage once asker has been answered and gone back to work.
func buildAnswered() []proto.Message {
	rows := paneltest.BuildTriage()
	rows[2] = paneltest.TriageAgent("asker", agentv1.AgentState_AGENT_STATE_THINKING, 3)

	return rows
}

// findTreeLine is the line of the frame the tree's workflow is rendered on.
func findTreeLine(frame string) int {
	return slices.IndexFunc(paneltest.SplitLines(frame), func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "backend") })
}
