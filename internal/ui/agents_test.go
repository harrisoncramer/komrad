package ui_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	loopv1 "komrad/proto/gen/loop/v1"
	"komrad/proto/gen/loop/v1/loopv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestAgentsRows covers which rows the agents tree renders and where.
func TestAgentsRows(t *testing.T) {
	archivedWriter := newArchivedAgent("writer", "writer", "w1")

	tests := []struct {
		name string
		rows []proto.Message
		want []string
	}{
		{
			name: "an agent whose workflow is gone is not rendered",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newAgent("a2", "orphan", "vanished"),
			},
			want: []string{"backend", "└─ auth"},
		},
		{
			name: "an archived agent and workflow are left to the archive",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newArchivedWorkflow("w2", "old"),
				newAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "done", "w1"),
			},
			want: []string{"backend", "└─ auth"},
		},
		{
			name: "an empty workflow keeps its header",
			rows: []proto.Message{newLiveWorkflow("w1", "backend")},
			want: []string{"backend"},
		},
		{
			name: "agents nest under the workflow they belong to",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newLiveWorkflow("w2", "frontend"),
				newAgent("a1", "auth", "w1"),
				newAgent("a2", "ui", "w2"),
			},
			want: []string{"backend", "└─ auth", "frontend", "└─ ui"},
		},
		{
			name: "a reviewer is rendered under its task rather than where the listing put it",
			rows: buildReviewedTasks(),
			want: []string{"komrad", "├─ task-fix-gutter", "└─ task-writer", "   ├─ " + reviewerMark + "second-reviewer", "   └─ " + reviewerMark + "reviewer"},
		},
		{
			name: "a reviewer whose writer is archived stays under its task",
			rows: []proto.Message{
				newLiveWorkflow("w1", "komrad"),
				newAgentAt("fix-gutter", "w1", 2),
				archivedWriter,
				newTaskOf(archivedWriter),
				newReviewer("a3", "reviewer", "writer"),
				newReviewer("a4", "second-reviewer", "writer"),
			},
			want: []string{"komrad", "├─ fix-gutter", "└─ task-writer", "   ├─ " + reviewerMark + "second-reviewer", "   └─ " + reviewerMark + "reviewer"},
		},
		{
			name: "a hidden task gives its row to its workflow's count",
			rows: slices.Concat(
				[]proto.Message{newLiveWorkflow("w1", "backend"), newAgent("a1", "auth", "w1")},
				newAgentOnHiddenTask("a2", "chore"),
				newAgentOnHiddenTask("a3", "sweep"),
			),
			want: []string{"backend +2", "└─ auth"},
		},
		{
			name: "a hidden task takes every agent on it and counts once",
			rows: hideTaskOf(buildReviewedTasks(), "writer"),
			want: []string{"komrad +1", "└─ task-fix-gutter"},
		},
		{
			name: "an orchestrator is rendered on its workflow's row rather than as a row of its own",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newOrchestrator(),
				newAgent("a1", "telescope", "w1"),
			},
			want: []string{"backend ⠋", "└─ telescope"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, tt.rows...)

			assert.Equal(t, tt.want, paneltest.ListRows(harness.Frame()))
		})
	}

	t.Run("a branch renders nothing on the row", func(t *testing.T) {
		plainAgent := newAgent("a1", "worker", "w1")
		branchedAgent := newAgent("a1", "worker", "w1")
		branch := newTaskOf(branchedAgent)
		branch.Branch = xptr.Ptr("agent/a1")

		plain := openAgents(t, newLiveWorkflow("w1", "backend"), plainAgent, newTaskOf(plainAgent))
		branched := openAgents(t, newLiveWorkflow("w1", "backend"), branchedAgent, branch)

		assert.Equal(t, paneltest.ListRows(plain.Frame()), paneltest.ListRows(branched.Frame()))
	})

	t.Run("an empty panel welcomes a newcomer and says where to start", func(t *testing.T) {
		harness := openAgents(t)

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, frame, "Welcome, komrad!")
		assert.Contains(t, frame, "Press 'space' then 'w' to set up your first workflow")
		assert.Empty(t, paneltest.ListRows(frame))
		paneltest.RequireGolden(t, "agents-welcome", harness.RawFrame())
	})
}

// TestAgentsShowHidden covers the hide menu, which hides the hovered task and shows and puts away
// the hidden ones.
func TestAgentsShowHidden(t *testing.T) {
	auth := newAgent("a1", "auth", "w1")
	ui := newAgent("a3", "ui", "w2")
	rows := slices.Concat(
		[]proto.Message{newLiveWorkflow("w1", "backend"), newLiveWorkflow("w2", "frontend"), auth, newTaskOf(auth)},
		newAgentOnHiddenTask("a2", "chore"),
		[]proto.Message{ui, newTaskOf(ui)},
	)

	t.Run("a hidden task comes back under its own workflow and the cursor keeps its row", func(t *testing.T) {
		harness := openAgents(t, rows...)
		harness.Press("j", "j")
		require.Equal(t, "└─ task-a3", paneltest.HoveredRow(harness.Frame()))

		harness.Press("space", "h", "H")

		frame := harness.Frame()
		assert.Equal(t, []string{"backend", "├─ task-a2" + hiddenMark, "└─ task-a1", "frontend", "└─ task-a3"}, paneltest.ListRows(frame))
		assert.Equal(t, "└─ task-a3", paneltest.HoveredRow(frame))
	})

	t.Run("a hidden task comes back with every agent on it", func(t *testing.T) {
		harness := openAgents(t, hideTaskOf(buildReviewedTasks(), "writer")...)

		harness.Press("k", "space", "h", "H")

		assert.Equal(t, []string{"komrad", "├─ task-fix-gutter", "└─ task-writer" + hiddenMark, "   ├─ " + reviewerMark + "second-reviewer", "   └─ " + reviewerMark + "reviewer"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("the hide entry hides the hovered task", func(t *testing.T) {
		harness := openAgents(t, rows...)

		harness.Press("space", "h", "h")

		harness.AssertSentTo(taskv1connect.TaskServiceSetTaskHiddenProcedure, &taskv1.SetTaskHiddenRequest{Task: "task-a1", Hidden: true})
	})

	t.Run("the hide entry unhides a revealed task", func(t *testing.T) {
		harness := openAgents(t, rows...)

		harness.Press("space", "h", "H", "k", "space", "h", "h")

		harness.AssertSentTo(taskv1connect.TaskServiceSetTaskHiddenProcedure, &taskv1.SetTaskHiddenRequest{Task: "task-a2", Hidden: false})
	})

	t.Run("a selection covering a whole task hides it", func(t *testing.T) {
		harness := openAgents(t, buildReviewedTasks()...)

		harness.Press("v", "space", "h", "h")

		harness.AssertSentTo(taskv1connect.TaskServiceSetTaskHiddenProcedure, &taskv1.SetTaskHiddenRequest{Task: "task-fix-gutter", Hidden: true})
	})

	t.Run("a selection reaching part of a task holds hide gray", func(t *testing.T) {
		harness := openAgents(t, buildReviewedTasks()...)

		harness.Press("v", "j", "j", "j", "v", "space", "h")

		assert.Regexp(t, readOffEntry("hide", "h"), harness.RawFrame())
	})

	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name:  "on a listed task the menu offers to hide it and show the rest",
			press: []string{},
			want:  []string{"› hide", "show hidden tasks"},
		},
		{
			name:  "on a revealed task the menu offers to unhide it and put the rest away",
			press: []string{"space", "h", "H", "k"},
			want:  []string{"› unhide", "put hidden tasks away"},
		},
		{
			name:  "on a workflow header only the toggle is live",
			press: []string{"k"},
			want:  []string{"› show hidden tasks"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, rows...)

			harness.Press(append(tt.press, "space", "h")...)

			frame := harness.Frame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
		})
	}
}

// TestAgentsSort covers ordering the agents tree by its column tabs.
func TestAgentsSort(t *testing.T) {
	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name: "created down is the fallback, the newest task first",
			want: []string{"gadgets", "└─ echo", "komrad", "├─ delta", "├─ Zulu", "└─ task-alpha", "   ├─ " + reviewerMark + "charlie", "   └─ " + reviewerMark + "bob"},
		},
		{
			name:  "created up is the oldest task first",
			press: []string{"f", "k"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ task-alpha", "│  ├─ " + reviewerMark + "bob", "│  └─ " + reviewerMark + "charlie", "├─ Zulu", "└─ delta"},
		},
		{
			name:  "activity down is the task whose writer changed state last first",
			press: []string{"f", "tab", "j"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ task-alpha", "│  ├─ " + reviewerMark + "bob", "│  └─ " + reviewerMark + "charlie", "└─ delta"},
		},
		{
			name:  "name up sorts a name in capitals as its lowercase",
			press: []string{"f", "tab", "tab", "k"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ task-alpha", "│  ├─ " + reviewerMark + "bob", "│  └─ " + reviewerMark + "charlie", "├─ delta", "└─ Zulu"},
		},
		{
			name:  "name down",
			press: []string{"f", "tab", "tab", "j"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ delta", "└─ task-alpha", "   ├─ " + reviewerMark + "charlie", "   └─ " + reviewerMark + "bob"},
		},
		{
			name:  "a flat list drops the headers and walks every workflow's tasks together",
			press: []string{"f", "shift+tab", "j"},
			want:  []string{"delta", "Zulu", "alpha", "  └ " + reviewerMark + "charlie", "  └ " + reviewerMark + "bob", "echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildSortable()...)

			harness.Press(tt.press...)

			assert.Equal(t, tt.want, paneltest.ListRows(harness.Frame()))
		})
	}

	t.Run("the cursor keeps its row as the tree is reordered", func(t *testing.T) {
		harness := openAgents(t, buildSortable()...)
		harness.Press("j", "j", "j", "j")
		require.Equal(t, "└─ task-alpha", paneltest.HoveredRow(harness.Frame()))

		harness.Press("f", "tab", "esc")
		assert.Equal(t, "└─ task-alpha", paneltest.HoveredRow(harness.Frame()), "walking to a tab reorders nothing")

		harness.Press("f", "tab", "j", "esc")
		assert.Equal(t, "├─ task-alpha", paneltest.HoveredRow(harness.Frame()), "activity down")

		harness.Press("f", "tab", "k", "esc")
		assert.Equal(t, "├─ task-alpha", paneltest.HoveredRow(harness.Frame()), "name up")
	})

	t.Run("the cursor keeps its agent across the flip to a flat list and back", func(t *testing.T) {
		harness := openAgents(t, buildSortable()...)
		require.Equal(t, "└─ echo", paneltest.HoveredRow(harness.Frame()))

		harness.Press("f", "shift+tab", "j", "esc")
		assert.Equal(t, "echo", paneltest.HoveredRow(harness.Frame()), "the flat list")

		harness.Press("f", "shift+tab", "k", "esc")
		assert.Equal(t, "└─ echo", paneltest.HoveredRow(harness.Frame()), "the tree again")
	})
}

// TestAgentsCursor covers where the cursor stands as keys move it and the watch reshapes the tree.
func TestAgentsCursor(t *testing.T) {
	rows := buildCursorable()

	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "one press of the top key waits for the second",
			press: []string{"j", "g"},
			want:  "├─ api",
		},
		{
			name:  "a key between the two drops the first press",
			press: []string{"j", "j", "g", "k", "g"},
			want:  "├─ api",
		},
		{
			name:  "the top key twice lands on the first row",
			press: []string{"j", "g", "g"},
			want:  "backend",
		},
		{
			name:  "the bottom key lands on the last row",
			press: []string{"G"},
			want:  "└─ ui",
		},
		{
			name: "the first listing lands on the first agent rather than the folder above it",
			want: "├─ auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, rows...)

			harness.Press(tt.press...)

			assert.Equal(t, tt.want, paneltest.HoveredRow(harness.Frame()))
		})
	}

	t.Run("the keys do nothing on an empty tree", func(t *testing.T) {
		harness := openAgents(t)

		harness.Press("G", "g", "g", "enter")

		assert.Empty(t, paneltest.HoveredRow(harness.Frame()))
		harness.AssertSent()
	})
}

// TestAgentsResume covers the open key resuming a dormant agent while the list stays in reach.
func TestAgentsResume(t *testing.T) {
	t.Parallel()

	t.Run("the cursor moves freely while a resume is in flight", func(t *testing.T) {
		t.Parallel()

		other := newAgent("a2", "other", "w1")
		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"), other)
		answerResume(harness)
		harness.Hold(agentv1connect.AgentServiceResumeAgentProcedure)

		harness.Press("j", "j")
		require.Equal(t, "└─ old", paneltest.HoveredRow(harness.Frame()))
		harness.Press("enter", "k")
		moved := paneltest.HoveredRow(harness.Frame())
		require.NotEqual(t, "  old", moved)

		harness.Release(agentv1connect.AgentServiceResumeAgentProcedure)

		assert.Equal(t, moved, paneltest.HoveredRow(harness.Frame()), "the landing leaves the cursor where it was moved")
	})

	t.Run("the open key again over an agent still resuming sends nothing", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)
		harness.Hold(agentv1connect.AgentServiceResumeAgentProcedure)

		harness.Press("j", "enter", "enter")
		harness.Release(agentv1connect.AgentServiceResumeAgentProcedure)

		assert.Len(t, harness.Sent(agentv1connect.AgentServiceResumeAgentProcedure), 1)
		assert.Empty(t, harness.Tmux().SelectedPanes())
	})
	t.Run("a resume says the agent is back up only once its pane decides its state", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.Press("j", "enter")
		require.NotContains(t, readOpenMessages(harness), "old is back up", "the write landing says nothing yet")

		settingUp := newRunning()
		settingUp.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), settingUp))
		require.NotContains(t, readOpenMessages(harness), "old is back up", "a CLI still starting is not back up")
		require.Contains(t, harness.Frame(), "Its CLI is coming up in a new pane.")

		settledBeforeAlive := newRunning()
		settledBeforeAlive.Alive = xptr.Ptr(false)
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), settledBeforeAlive))
		require.NotContains(t, readOpenMessages(harness), "old is back up", "an idle row not yet alive is not back up")

		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), newRunning()))
		assert.Contains(t, readOpenMessages(harness), "old is back up")
	})

	t.Run("a resume whose pane went before its CLI came up is never said to be back up", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.Press("j", "enter")

		settingUp := newRunning()
		settingUp.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), settingUp))
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), newRunning()))

		assert.NotContains(t, readOpenMessages(harness), "old is back up")
	})

	t.Run("a refused resume never says the agent is back up", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		harness.Answer(agentv1connect.AgentServiceResumeAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("held"))
		})

		harness.Press("j", "enter")
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "api"), newRunning()))

		assert.NotContains(t, readOpenMessages(harness), "old is back up")
	})
}

// TestAgentsCursorAcrossListings covers the cursor keeping its row as the watch reshapes the tree.
func TestAgentsCursorAcrossListings(t *testing.T) {
	rows := buildCursorable()

	tests := []struct {
		name  string
		press []string
		next  []proto.Message
		want  string
	}{
		{
			name:  "a listing that empties the tree leaves the screen standing",
			press: []string{"j"},
			want:  "",
		},
		{
			name:  "the cursor follows its agent rather than its index",
			press: []string{"j"},
			next: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgentAt("api", "w1", 3),
				newAgentAt("auth", "w1", 2),
			},
			want: "├─ api",
		},
		{
			name:  "an agent created elsewhere lands where the listing put it",
			press: []string{"j"},
			next: append(slices.Clone(rows),
				newAgentAt("new", "w1", 9),
			),
			want: "├─ api",
		},
		{
			name:  "a folder archived elsewhere leaves the cursor on the folder it named",
			press: []string{"G", "k"},
			next: []proto.Message{
				newLiveWorkflow("w2", "frontend"),
				newAgentAt("ui", "w2", 0),
			},
			want: "frontend",
		},
		{
			name:  "a folder sorting in above keeps the cursor on its agent",
			press: []string{"G"},
			next: append(slices.Clone(rows),
				newLiveWorkflow("w0", "api"),
				newAgentAt("spike", "w0", 4),
			),
			want: "└─ ui",
		},
		{
			name:  "the last agent under a folder going lands on the next agent rather than its header",
			press: []string{"j", "j"},
			next: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newLiveWorkflow("w2", "frontend"),
				newAgentAt("auth", "w1", 3),
				newAgentAt("api", "w1", 2),
				newAgentAt("ui", "w2", 0),
			},
			want: "└─ ui",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, rows...)
			harness.Press(tt.press...)

			harness.Watch(paneltest.Snapshot(tt.next...))

			frame := harness.Frame()
			assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
			assert.Equal(t, tt.want, paneltest.HoveredRow(frame))
		})
	}
}

// TestAgentsRecord covers the record rendered under the list for the hovered row.
func TestAgentsRecord(t *testing.T) {
	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		want       map[string]string
		wantAbsent []string
	}{
		{
			name:       "a workflow shows how many agents it holds and has no session of its own",
			rows:       buildRecorded(),
			press:      []string{"k"},
			want:       map[string]string{"name": "backend", "agents": "1"},
			wantAbsent: []string{"session_id"},
		},
		{
			name: "an agent shows what it was launched with",
			rows: buildRecorded(),
			want: map[string]string{
				"directory":  "/repo",
				"agent_id":   "a1",
				"session_id": "abc",
			},
		},
		{
			name: "an agent shows the hooks komrad installs in its CLI and the tool it is running",
			rows: buildRecordedWith(func(agent *agentv1.Agent) {
				agent.Hooks = []string{"PreToolUse", "Stop"}
				agent.ReportedReason = "Bash"
			}),
			want: map[string]string{
				"hooks": "PreToolUse, Stop",
				"tool":  "Bash",
			},
		},
		{
			name:       "an agent running no tool shows none",
			rows:       buildRecorded(),
			want:       map[string]string{"hooks": "—"},
			wantAbsent: []string{"tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, tt.rows...)

			harness.Press(append(tt.press, widenListPresses...)...)

			frame := harness.Frame()
			for label, value := range tt.want {
				assert.Equal(t, value, paneltest.RecordField(frame, label), label)
			}
			for _, label := range tt.wantAbsent {
				assert.Negative(t, paneltest.RecordLine(frame, label), label)
			}
		})
	}
}

// TestAgentsRecordFocus covers the focus key handing the keys to the record and back.
func TestAgentsRecordFocus(t *testing.T) {
	t.Run("movement runs down the fields while the list keeps its row", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Press("pick:record", "j")

		frame := harness.Frame()
		assert.Regexp(t, `^directory\s+/repo$`, paneltest.RecordCursor(frame))
		assert.Contains(t, paneltest.Legend(frame), "esc back to the list")

		harness.Press("pick:list")

		frame = harness.Frame()
		assert.Empty(t, paneltest.RecordCursor(frame))
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(frame), "the list kept its row")

		harness.Press("k")
		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()), "the list has the keys again")
	})

	t.Run("the wheel over the record hands it the keys and over the list hands them back", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		y := paneltest.RecordLine(harness.Frame(), "agent")
		require.GreaterOrEqual(t, y, 0)

		harness.Send(tea.MouseWheelMsg{
			X:      1,
			Y:      y,
			Button: tea.MouseWheelDown,
		})
		harness.Advance(wheelFrame)

		frame := harness.Frame()
		assert.Regexp(t, `^directory\s+/repo$`, paneltest.RecordCursor(frame))

		harness.Send(tea.MouseWheelMsg{
			X:      1,
			Y:      9,
			Button: tea.MouseWheelUp,
		})

		frame = harness.Frame()
		assert.Empty(t, paneltest.RecordCursor(frame))
		assert.Equal(t, "backend", paneltest.HoveredRow(frame), "the list has the keys again")
	})

	t.Run("one cursor is rendered at a time", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, 1, countCursors(harness.Frame()))

		harness.Press("pick:record")

		frame := harness.Frame()
		assert.Equal(t, 1, countCursors(frame))
		assert.NotEmpty(t, paneltest.RecordCursor(frame), "the cursor is the record's")
	})

	t.Run("another row's record opens at its top", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Press("pick:record", "j", "j", "pick:list", "k", "pick:record")

		assert.Regexp(t, `^name\s+backend$`, paneltest.RecordCursor(harness.Frame()))
	})

	t.Run("the keys that act on a row stand down while the record has them", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.Press("pick:record")
		before := harness.Frame()
		sent := harness.Requests()

		harness.Press("r", "n", "c")

		assert.Equal(t, before, harness.Frame())
		harness.AssertSent(sent...)
	})

	t.Run("nothing listed gives the record no letter", func(t *testing.T) {
		harness := openAgents(t)

		harness.Press("ctrl+space", "j")

		assert.Empty(t, harness.PickLetter("record"))
	})

	t.Run("a record shortened under the cursor holds it on its last field", func(t *testing.T) {
		rows := buildRecorded()
		agent, _ := rows[1].(*agentv1.Agent)
		task := newTaskOf(agent)
		task.Branch = xptr.Ptr("agent/auth")
		rows = append(rows, task)
		harness := openAgents(t, rows...)
		harness.Press("pick:record", "G")
		require.Regexp(t, `^task_id\s`, paneltest.RecordCursor(harness.Frame()))

		task.Branch = nil
		harness.Watch(paneltest.Snapshot(rows...))

		assert.Regexp(t, `^task_id\s`, paneltest.RecordCursor(harness.Frame()))
	})

	t.Run("the row going from under a focused record renders the next from its top", func(t *testing.T) {
		rows := buildRecorded()
		harness := openAgents(t, rows...)
		harness.Press("pick:record", "j", "j", "j")

		harness.Watch(paneltest.Snapshot(rows[0]))
		require.Regexp(t, `^name\s+backend$`, paneltest.RecordCursor(harness.Frame()))

		harness.Press("j")
		assert.Regexp(t, `^agents\s+0$`, paneltest.RecordCursor(harness.Frame()))
	})
}

// TestAgentsRecordCopy covers the copy key taking the field under the record's cursor.
func TestAgentsRecordCopy(t *testing.T) {
	t.Run("a clipboard that refuses the write says so", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		harness.SetClipboardErr(errors.New("no clipboard utility found"))
		answerWorktreeReads(harness)
		harness.Watch(paneltest.Snapshot(buildRecorded()...))

		harness.Press("pick:record", "y")
		harness.Advance(time.Second)
		opened := readOpenMessages(harness)
		require.Contains(t, opened, "Could not copy")
		require.Contains(t, opened, "no clipboard utility found")
		harness.Press("ctrl+g")
		settleFlash(harness)
		refused := harness.Frame()
		require.Contains(t, readEngineLine(refused), "✗ 1")

		harness.SetClipboardErr(nil)
		harness.Press("y")
		harness.Advance(time.Second)

		assert.Equal(t, refused, harness.Frame(), "a copy that worked reports nothing of its own")
	})

	tests := []struct {
		name  string
		open  func(*testing.T) *paneltest.Panel
		press []string
		want  string
	}{
		{
			name: "the whole value is taken rather than the clipped line it is rendered on",
			open: func(t *testing.T) *paneltest.Panel {
				t.Helper()

				rows := buildRecorded()
				agent, _ := rows[1].(*agentv1.Agent)
				agent.Flags = strings.Repeat("--append-system-prompt read every file on the branch ", 4)
				harness := paneltest.Start(t, paneltest.Params{})
				answerWorktreeReads(harness)
				harness.Watch(paneltest.Snapshot(rows...))

				return harness
			},
			press: []string{"pick:record", "j", "j"},
			want:  strings.Repeat("--append-system-prompt read every file on the branch ", 4),
		},
		{
			name: "a narrowed record copies from the rows that are left",
			open: func(t *testing.T) *paneltest.Panel {
				t.Helper()

				return openAgentRecord(t)
			},
			press: []string{"/", "_", "i", "d", "tab", "enter", "G"},
			want:  "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := tt.open(t)

			harness.Press(append(tt.press, "y")...)

			assert.Equal(t, []string{tt.want}, harness.Copied())
		})
	}

	t.Run("the field on screen is taken after the row goes from under the record", func(t *testing.T) {
		rows := buildRecorded()
		harness := openCopyRecord(t)
		harness.Press("pick:record", "j", "j", "j")

		harness.Watch(paneltest.Snapshot(rows[0]))
		harness.Press("y")

		assert.Equal(t, []string{"backend"}, harness.Copied())
	})
}

// TestAgentsRecordHeight covers how many lines the record takes from the sidebar.
func TestAgentsRecordHeight(t *testing.T) {
	tests := []struct {
		name          string
		height        int
		detailsHeight float64
		want          int
	}{
		{
			name:   "a pane without room for the chrome and a field closes the record",
			height: 8,
			want:   0,
		},
		{
			name:          "a share that snapshots down to no field closes it too",
			height:        19,
			detailsHeight: 0.05,
			want:          0,
		},
		{
			name:          "more than the pane holds is capped at half of it",
			height:        24,
			detailsHeight: 40,
			want:          10,
		},
		{
			name:          "a share of the pane",
			height:        24,
			detailsHeight: 0.25,
			want:          6,
		},
		{
			name:          "a whole number of rows",
			height:        24,
			detailsHeight: 8,
			want:          8,
		},
		{
			name:   "the default is nine lines however tall the pane",
			height: 60,
			want:   9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{
				Height:        tt.height,
				DetailsHeight: tt.detailsHeight,
			})
			answerWorktreeReads(harness)
			harness.Watch(paneltest.Snapshot(buildRecorded()...))

			frame := harness.Frame()
			assert.Equal(t, tt.want > 0, paneltest.IsRecordRendered(frame))
			assert.Len(t, paneltest.Record(frame), tt.want)
		})
	}
}

// TestAgentsRecordFocusAndSize covers the record giving up the keys when the pane has no room to render it.
func TestAgentsRecordFocusAndSize(t *testing.T) {
	rows := append(buildRecorded(), newAgentAt("billing", "w1", 1))

	tests := []struct {
		name   string
		params paneltest.Params
	}{
		{
			name:   "a pane too short to render the record takes no letter",
			params: paneltest.Params{Height: 8},
		},
		{
			name: "a share too small for a field takes no letter",
			params: paneltest.Params{
				Height:        19,
				DetailsHeight: 0.05,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, tt.params)
			answerWorktreeReads(harness)
			harness.Watch(paneltest.Snapshot(rows...))

			harness.Press("ctrl+space", "j")

			assert.Empty(t, harness.PickLetter("record"))
		})
	}

	t.Run("a shrink takes the keys back and a grow does not return them", func(t *testing.T) {
		harness := openAgents(t, rows...)
		harness.Press("pick:record", "j")
		require.NotEmpty(t, paneltest.RecordCursor(harness.Frame()))

		harness.Send(tea.WindowSizeMsg{
			Width:  paneltest.DefaultWidth,
			Height: 4,
		})
		harness.Press("j")
		harness.Send(tea.WindowSizeMsg{
			Width:  paneltest.DefaultWidth,
			Height: paneltest.DefaultHeight,
		})

		frame := harness.Frame()
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(frame), "the list answered the key pressed while it was small")
		assert.Empty(t, paneltest.RecordCursor(frame))

		harness.Press("k")
		assert.Equal(t, "├─ billing", paneltest.HoveredRow(harness.Frame()), "the keys are still with the list")

		harness.Press("pick:record")
		assert.NotEmpty(t, paneltest.RecordCursor(harness.Frame()), "until the focus key is pressed again")
	})
}

// TestAgentsRecordWorktree covers the task's record carrying what the server read of its directory.
func TestAgentsRecordWorktree(t *testing.T) {
	unreadable := buildWorktreeRead()
	unreadable.Worktree.Readable = false

	tests := []struct {
		name       string
		answer     paneltest.AnswerFunc
		want       map[string]string
		wantAbsent []string
	}{
		{
			name:       "a directory komrad does not hold renders no worktree rows and no error",
			answer:     refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"),
			wantAbsent: []string{"worktree", "size", "changes"},
		},
		{
			name:   "a directory git refuses says why",
			answer: refuseWith(connect.CodeInternal, "git exploded"),
			want:   map[string]string{"worktree": "could not read: git exploded"},
		},
		{
			name:       "a registration git cannot read has a size and no standing",
			answer:     answerWith(unreadable),
			want:       map[string]string{"size": "1.2G, measured 5m ago"},
			wantAbsent: []string{"changes", "commits"},
		},
		{
			name:   "a readable worktree says what is uncommitted and leaves the base to the worktree screen",
			answer: answerWith(buildWorktreeRead()),
			want: map[string]string{
				"changes":  "2 modified, 1 untracked",
				"landed":   "1 commit the base already holds",
				"sequence": "a merge or rebase is part-way through",
			},
			wantAbsent: []string{"remote", "uncommitted", "base", "also here"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{})
			harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, tt.answer)
			harness.Watch(paneltest.Snapshot(buildRecordedTask()...))

			harness.Press(append([]string{"pick:record"}, widenListPresses...)...)

			frame := harness.Frame()
			for label, value := range tt.want {
				assert.Equal(t, value, paneltest.RecordField(frame, label), label)
			}
			for _, label := range tt.wantAbsent {
				assert.Negative(t, paneltest.RecordLine(frame, label), label)
			}
		})
	}

	t.Run("a flat list puts the task's checkout on the agent's record", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.Watch(paneltest.Snapshot(buildRecordedTask()...))

		harness.Press("f", "shift+tab", "j", "esc")
		harness.Advance(time.Second)
		harness.Press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.Frame()
		assert.Equal(t, "agent/auth", paneltest.RecordField(frame, "branch"))
		assert.Equal(t, "2 modified, 1 untracked", paneltest.RecordField(frame, "changes"))
	})

	t.Run("the top rule carries where the directory stands left of the lines the branch changed", func(t *testing.T) {
		changed := &taskv1.DiffStat{Added: 200, Removed: 100}
		unpushed := buildWorktreeRead()
		unpushed.Modified, unpushed.Untracked, unpushed.Unpulled = nil, nil, 0
		unpulledLanded := buildWorktreeRead()
		unpulledLanded.UnpulledLanded = true
		tests := []struct {
			name     string
			read     *worktreev1.GetWorktreeResponse
			diffStat *taskv1.DiffStat
			presses  []string
			want     string
		}{
			{
				name: "a task with no base to count against carries nothing",
			},
			{
				name:     "a branch with nothing changed reads clean",
				diffStat: &taskv1.DiffStat{},
				want:     "clean",
			},
			{
				name:     "a branch with changes counts the lines added and removed",
				diffStat: changed,
				want:     "+200 −100",
			},
			{
				name: "a directory with nothing waiting carries only the lines",
				read: &worktreev1.GetWorktreeResponse{
					Worktree: &worktreev1.Worktree{
						Dir:      "/repo",
						Readable: true,
						Branch:   "agent/x",
					},
					BaseRef: "origin/main",
					Pushed:  true,
				},
				diffStat: changed,
				want:     "+200 −100",
			},
			{
				name: "a branch never pushed says so",
				read: &worktreev1.GetWorktreeResponse{
					Worktree: &worktreev1.Worktree{
						Dir:      "/repo",
						Readable: true,
						Branch:   "agent/x",
					},
					BaseRef: "origin/main",
					Ahead:   1,
				},
				diffStat: changed,
				want:     "not pushed · +200 −100",
			},
			{
				name:     "commits not on origin are counted",
				read:     unpushed,
				diffStat: changed,
				want:     "↑2 unpushed · +200 −100",
			},
			{
				name:     "a narrow rule gives up the leftmost of what is waiting first",
				read:     buildWorktreeRead(),
				diffStat: changed,
				want:     "↓1 unpulled · +200 −100",
			},
			{
				name:     "a wide rule carries every change waiting in the directory",
				read:     buildWorktreeRead(),
				diffStat: changed,
				presses:  widenListPresses,
				want:     "2 modified · 1 untracked · ↑2 unpushed · ↓1 unpulled · +200 −100",
			},
			{
				name:     "unpulled commits the base already holds are left off",
				read:     unpulledLanded,
				diffStat: changed,
				presses:  widenListPresses,
				want:     "2 modified · 1 untracked · ↑2 unpushed · +200 −100",
			},
			{
				name: "a branch the base already holds whole has nothing to push",
				read: &worktreev1.GetWorktreeResponse{
					Worktree: &worktreev1.Worktree{
						Dir:      "/repo",
						Readable: true,
						Branch:   "agent/x",
					},
					BaseRef:        "origin/main",
					Pushed:         true,
					Unpushed:       2,
					Unpulled:       1,
					UnpulledLanded: true,
				},
				diffStat: changed,
				want:     "+200 −100",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				recorded := buildRecorded()
				task := newTaskOf(recorded[1].(*agentv1.Agent))
				task.DiffStat = tt.diffStat
				task.Cwd = xptr.Ptr("/repo")
				recorded = append(recorded, task)
				harness := paneltest.Start(t, paneltest.Params{})
				if tt.read == nil {
					harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"))
				} else {
					harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(tt.read))
				}
				harness.Watch(paneltest.Snapshot(recorded...))

				harness.Press(tt.presses...)
				harness.Advance(time.Second)

				assert.Equal(t, tt.want, readRecordTag(harness.Frame()))
			})
		}
	})

	t.Run("the focus key reads the directory again", func(t *testing.T) {
		harness := openAgents(t, buildRecordedTask()...)
		harness.Advance(time.Second)
		harness.AssertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(1)...)

		harness.Press("pick:record")

		harness.AssertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(2)...)
	})

	t.Run("looking at a live agent wakes the cwd sync to measure it", func(t *testing.T) {
		tests := []struct {
			name    string
			presses []string
		}{
			{
				name:    "the cursor coming to rest",
				presses: []string{"k", "j"},
			},
			{
				name:    "the focus key",
				presses: []string{"pick:record"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, buildRecorded()...)
				harness.AssertSentTo(loopv1connect.LoopServiceWakeLoopProcedure)

				harness.Press(tt.presses...)
				harness.Advance(time.Second)

				harness.AssertSentTo(loopv1connect.LoopServiceWakeLoopProcedure, &loopv1.WakeLoopRequest{Loop: loopv1.Loop_LOOP_CWD_SYNC})
			})
		}
	})

	t.Run("a refused reread keeps the last answer beside the refusal", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.Watch(paneltest.Snapshot(buildRecordedTask()...))
		harness.Advance(time.Second)
		harness.Refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("context deadline exceeded")))

		harness.Press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.Frame()
		assert.Equal(t, "could not read: context deadline exceeded", paneltest.RecordField(frame, "worktree"))
		assert.Equal(t, "2 modified, 1 untracked", paneltest.RecordField(frame, "changes"))
	})

	t.Run("the cursor coming to rest asks again only for a read gone stale", func(t *testing.T) {
		tests := []struct {
			name string
			age  time.Duration
			want int
		}{
			{
				name: "a fresh read is not asked for again",
				want: 1,
			},
			{
				name: "a stale read is",
				age:  2 * time.Minute,
				want: 2,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, buildRecordedTask()...)
				harness.Advance(time.Second)
				harness.Advance(tt.age)

				harness.Press("k", "j")
				harness.Advance(time.Second)

				harness.AssertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(tt.want)...)
			})
		}
	})

	t.Run("a directory komrad does not hold is never asked about twice", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"))
		harness.Watch(paneltest.Snapshot(buildRecordedTask()...))

		harness.Press("pick:record", "pick:list", "k", "j")
		harness.Advance(time.Second)

		harness.AssertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(1)...)
	})

	t.Run("a focused record grows to show every field and shrinks back", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{DetailsHeight: 5})
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.Watch(paneltest.Snapshot(buildRecordedTask()...))
		harness.Advance(time.Second)
		share := len(paneltest.Record(harness.Frame()))
		require.Negative(t, paneltest.RecordLine(harness.Frame(), "created"), "the last fields are past the share")

		harness.Press("pick:record")

		frame := harness.Frame()
		assert.Positive(t, paneltest.RecordLine(frame, "created"))
		assert.NotContains(t, strings.Join(paneltest.Record(frame), "\n"), "more", "nothing is left out")
		assert.Len(t, paneltest.SplitLines(frame), paneltest.DefaultHeight)
		assert.Equal(t, []string{"backend", "└─ task-a1"}, paneltest.ListRows(frame), "the list keeps its rows")

		harness.Press("pick:list")
		assert.Len(t, paneltest.Record(harness.Frame()), share)
	})

	t.Run("a focused record taller than the pane grows only as far as the room", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{Height: 16})
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.Watch(paneltest.Snapshot(buildRecordedTask()...))
		harness.Press("k")
		harness.Advance(time.Second)

		harness.Press("pick:record")

		frame := harness.Frame()
		assert.Len(t, paneltest.SplitLines(frame), 16)
		assert.Contains(t, strings.Join(paneltest.Record(frame), "\n"), "more")
	})
}

// TestAgentsRecordSearch covers the one search over a focused record.
func TestAgentsRecordSearch(t *testing.T) {
	t.Run("typing lights the match and moves nothing until enter walks onto it", func(t *testing.T) {
		harness := openWorktreeRecord(t, paneltest.Params{})

		harness.Press("/", "s", "i", "z")

		frame := harness.Frame()
		assert.Regexp(t, `^directory\s`, paneltest.RecordCursor(frame))
		assert.Contains(t, frame, "/ siz", "the header's search line is the record's")

		harness.Press("enter")
		assert.Regexp(t, `^size\s`, paneltest.RecordCursor(harness.Frame()))
	})

	t.Run("n and N step between the matches once a search stands", func(t *testing.T) {
		harness := openAgentRecord(t)

		harness.Press("n")
		require.Regexp(t, `^agent\s`, paneltest.RecordCursor(harness.Frame()), "nothing stands to step through")

		harness.Press("/", "_", "i", "d", "enter")
		assert.Regexp(t, `^agent_id\s`, paneltest.RecordCursor(harness.Frame()))

		harness.Press("n")
		assert.Regexp(t, `^session_id\s`, paneltest.RecordCursor(harness.Frame()))

		harness.Press("N")
		assert.Regexp(t, `^agent_id\s`, paneltest.RecordCursor(harness.Frame()))
	})

	t.Run("tab narrows the record to the fields it matches and the rows under them", func(t *testing.T) {
		harness := openAgentRecord(t)

		harness.Press(append([]string{"/", "_", "i", "d", "tab", "enter"}, widenListPresses...)...)

		frame := harness.Frame()
		assert.Contains(t, frame, "▽ _id")
		assert.Equal(t, []string{
			"› agent_id   a1",
			"  session_id abc",
		}, slices.DeleteFunc(paneltest.Record(frame), isBlank))
	})

	t.Run("a filter nothing matches says so and leaves the list alone", func(t *testing.T) {
		harness := openAgentRecord(t)

		harness.Press("/", "z", "z", "z", "tab", "enter")

		frame := harness.Frame()
		assert.Contains(t, strings.Join(paneltest.Record(frame), "\n"), "nothing matches")
		assert.Contains(t, paneltest.Legend(frame), "back to the list", "the record keeps the keys")
		assert.Equal(t, []string{"backend", "└─ auth"}, paneltest.ListRows(frame))
	})

	t.Run("escape abandons the input, then clears the search, then hands the keys back", func(t *testing.T) {
		harness := openWorktreeRecord(t, paneltest.Params{})

		harness.Press("/", "s", "i", "z", "esc")

		frame := harness.Frame()
		assert.Equal(t, "/", readSearch(frame))
		assert.Regexp(t, `^directory\s`, paneltest.RecordCursor(frame), "the record keeps the keys and its cursor")

		harness.Press("/", "s", "i", "z", "enter", "esc")

		frame = harness.Frame()
		assert.Equal(t, "/", readSearch(frame), "the first escape clears the search")
		assert.NotEmpty(t, paneltest.RecordCursor(frame))

		harness.Press("esc")
		assert.Empty(t, paneltest.RecordCursor(harness.Frame()), "the second hands the keys back")
	})

	t.Run("a search standing when the keys go keeps narrowing the record", func(t *testing.T) {
		harness := openWorktreeRecord(t, paneltest.Params{})

		harness.Press("/", "s", "i", "z", "tab", "enter", "pick:record")

		frame := harness.Frame()
		assert.Equal(t, "▽ siz", readSearch(frame))
		assert.Len(t, slices.DeleteFunc(paneltest.Record(frame), isBlank), 1)
	})

	t.Run("the search goes with the screen", func(t *testing.T) {
		harness := openWorktreeRecord(t, paneltest.Params{})

		harness.Press("/", "s", "i", "z", "enter", "pick:record", "tab", "shift+tab")

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Equal(t, "/", readSearch(frame))
	})

	t.Run("a focused record keeps the keys through its search in a tight pane", func(t *testing.T) {
		tests := []struct {
			name       string
			params     paneltest.Params
			press      []string
			wantLegend bool
		}{
			{
				name: "a pane the share only just fits",
				params: paneltest.Params{
					Width:  80,
					Height: 13,
				},
			},
			{
				name: "one row taller",
				params: paneltest.Params{
					Width:  80,
					Height: 14,
				},
			},
			{
				name: "a narrow pane with the legend expanded",
				params: paneltest.Params{
					Width:  38,
					Height: 44,
				},
				press:      []string{"?"},
				wantLegend: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := paneltest.Start(t, tt.params)
				harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
				harness.Watch(paneltest.Snapshot(buildRecorded()...))
				harness.Press(tt.press...)
				if tt.wantLegend {
					require.Contains(t, paneltest.Legend(harness.Frame()), "hide help", "the legend is expanded")
				}
				harness.Press("pick:record")
				require.NotEmpty(t, paneltest.RecordCursor(harness.Frame()), "the share fits, so the record takes the keys")

				harness.Press("/", "a")

				frame := harness.Frame()
				assert.NotEmpty(t, paneltest.Record(frame), "the record is still rendered with its search line")
				assert.Len(t, paneltest.SplitLines(frame), tt.params.Height)

				harness.Press("enter")

				frame = harness.Frame()
				assert.NotEmpty(t, paneltest.RecordCursor(frame), "enter kept the search rather than reaching the list")
				assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))

				harness.Press("esc", "esc")
				assert.Empty(t, paneltest.RecordCursor(harness.Frame()), "two escapes clear the search and hand the keys back")
			})
		}
	})

	t.Run("a slash after a shrink took the keys opens the list's search", func(t *testing.T) {
		harness := openWorktreeRecord(t, paneltest.Params{
			Width:  80,
			Height: 30,
		})
		harness.Send(tea.WindowSizeMsg{
			Width:  80,
			Height: 11,
		})

		harness.Press("/", "a", "g", "e", "n", "t", "3")

		assert.Contains(t, harness.Frame(), "/ agent3")
	})
}

// TestAgentsSearchAcrossSections covers one search standing over both the list and the record.
func TestAgentsSearchAcrossSections(t *testing.T) {
	t.Run("a filter typed on the list narrows the list alone", func(t *testing.T) {
		harness := openAgents(t, append(buildRecorded(), newAgentAt("billing", "w1", 1))...)
		harness.Press("j")
		fields := len(paneltest.Record(harness.Frame()))

		harness.Press("/", "a", "u", "t", "h", "tab", "enter")

		frame := harness.Frame()
		assert.Equal(t, []string{"backend", "└─ auth"}, paneltest.ListRows(frame))
		assert.Len(t, paneltest.Record(frame), fields, "the record keeps every field")
	})

	t.Run("the focus key while typing jumps into the record's first match and back out moves nothing", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Press("/", "a", "b", "c", "pick:record")

		frame := harness.Frame()
		assert.Regexp(t, `^session_id\s+abc$`, paneltest.RecordCursor(frame))
		assert.Contains(t, frame, "/ abc", "the pattern stands")
		assert.NotContains(t, frame, "/ abc▏", "and the input is closed")

		harness.Press("pick:list")
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()), "the list never moved")

		harness.Press("esc", "/", "b", "a", "c", "k", "enter", "pick:record")
		assert.Regexp(t, `^name\s+backend$`, paneltest.RecordCursor(harness.Frame()), "enter walked the list onto the header before the jump")
	})

	t.Run("clearing the search where it opened puts the cursor back and from the record leaves it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.Press("k", "/", "a", "u", "t", "enter")
		require.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()))

		harness.Press("esc")
		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))

		harness.Press("/", "a", "u", "t", "enter", "pick:record", "esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, "/ aut")
		assert.NotEmpty(t, paneltest.RecordCursor(frame), "the record keeps the keys")

		harness.Press("pick:list")
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("the page keys move the record's cursor ten fields at a time", func(t *testing.T) {
		rows := buildRecorded()
		rows[0].(*workflowv1.Workflow).Worktree.CopyPaths = []string{".env"}
		harness := openAgents(t, rows...)
		harness.Press("k", "pick:record")
		first := paneltest.RecordCursor(harness.Frame())

		harness.Press("ctrl+d")
		assert.Equal(t, 10, findRecordCursor(harness.Frame()))

		harness.Press("ctrl+u")
		assert.Equal(t, first, paneltest.RecordCursor(harness.Frame()))

		harness.Press("G")
		last := paneltest.RecordCursor(harness.Frame())
		harness.Press("ctrl+d")
		assert.Equal(t, last, paneltest.RecordCursor(harness.Frame()), "a page past the end stays on the last field")
	})

	t.Run("the match keys walk only the section holding the keys", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Press("k", "/", "b", "a", "c", "k", "enter", "n")

		frame := harness.Frame()
		assert.Equal(t, "backend", paneltest.HoveredRow(frame), "n wraps to the list's only match")
		assert.Empty(t, paneltest.RecordCursor(frame))

		harness.Press("pick:record", "n")
		assert.Regexp(t, `^name\s+backend$`, paneltest.RecordCursor(harness.Frame()), "and in the record wraps to the record's")
	})

	t.Run("enter walks the list only onto a row the list matches", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Press("/", "b", "a", "c", "k")
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()), "typing moves nothing")

		harness.Press("enter")
		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))

		harness.Press("esc", "/", "a", "b", "c", "enter")
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()), "a pattern only the record matches leaves the list where it is")
	})

	t.Run("escape after the jump into the record clears the search and leaves both cursors", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.Press("/", "a", "b", "c", "pick:record")

		harness.Press("esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, "/ abc")
		assert.Regexp(t, `^session_id\s+abc$`, paneltest.RecordCursor(frame))
	})

	t.Run("a filter matching only a field empties the list and the record with it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Press("/", "a", "b", "c", "tab", "enter")

		frame := harness.Frame()
		assert.Empty(t, paneltest.ListRows(frame))
		assert.Empty(t, paneltest.RecordCursor(frame))
		assert.Contains(t, frame, "nothing matches")
	})
}

// TestAgentsFailedPlaceholder covers what a failed agent's stage says about why it failed.
func TestAgentsFailedPlaceholder(t *testing.T) {
	tests := []struct {
		name   string
		state  agentv1.AgentState
		reason string
		want   []string
	}{
		{
			name:   "a failed create says the reason inline",
			state:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			reason: "failed to open a window: tmux refused",
			want:   []string{"setup failed", "tmux refused"},
		},
		{
			name:   "a failed delete names the teardown",
			state:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP,
			reason: "take-the-stack-down: docker refused",
			want:   []string{"teardown failed", "docker refused"},
		},
		{
			name:  "a failure with no reason says so",
			state: agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			want:  []string{"no reason was recorded"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := newAgent("a1", "old", "w1")
			failed.State = tt.state
			failed.FailureReason = tt.reason
			harness := openAgents(t, newLiveWorkflow("w1", "api"), failed)
			harness.Press("j")

			frame := harness.Frame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
		})
	}
}

// TestAgentsLastScreen covers the saved screen rendered beside an agent that is not running.
func TestAgentsLastScreen(t *testing.T) {
	tests := []struct {
		name          string
		agent         *agentv1.Agent
		want          []string
		wantAbsent    []string
		wantScreenFor []string
	}{
		{
			name:          "a dormant agent renders its saved screen under how to resume it",
			agent:         newDormant("login"),
			want:          []string{"dormant · press enter to restore", "saved screen", "the migration is written"},
			wantScreenFor: []string{"a1"},
		},
		{
			name: "a failed agent renders its failure and no saved screen",
			agent: func() *agentv1.Agent {
				agent := newDormant("login")
				agent.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

				return agent
			}(),
			want:       []string{"setup failed"},
			wantAbsent: []string{"saved screen"},
		},
		{
			name:  "a running agent asks for no saved screen",
			agent: newThinking(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{})
			answerWorktreeReads(harness)
			answerLastScreens(harness, map[string]string{"a1": "⏺ the migration is written"})
			harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), tt.agent))
			harness.Press("j")
			harness.Advance(time.Second)

			frame := harness.Frame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.AssertSentTo(agentv1connect.AgentServiceGetLastScreenProcedure, xslice.Map(tt.wantScreenFor, func(id string) proto.Message {
				return &agentv1.GetLastScreenRequest{AgentId: id}
			})...)
		})
	}

	t.Run("an agent that runs and stops again under the cursor renders its newer screen", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		answerWorktreeReads(harness)
		answerLastScreens(harness, map[string]string{"a1": "⏺ the migration is written"})
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newDormant("login")))
		harness.Press("j")
		harness.Advance(time.Second)
		require.Contains(t, harness.Frame(), "the migration is written")

		live := newDormant("login")
		live.Alive = xptr.Ptr(true)
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), live))
		answerLastScreens(harness, map[string]string{"a1": "⏺ the tests pass"})
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newDormant("login")))

		assert.Contains(t, harness.Frame(), "the tests pass")
	})

	t.Run("a saved screen taller than the window opens on its bottom", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		answerWorktreeReads(harness)
		answerLastScreens(harness, map[string]string{
			"a1": "❯ the first ask\n" + strings.Repeat("working through it\n", 60) + "⏺ the migration is written",
		})
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newDormant("login")))
		harness.Press("j")
		harness.Advance(time.Second)

		frame := harness.Frame()
		assert.Contains(t, frame, "the migration is written")
		assert.NotContains(t, frame, "the first ask")
	})
}

// TestAgentsEdit covers opening your editor on an agent's worktree from its row.
func TestAgentsEdit(t *testing.T) {
	t.Run("the edit key opens the worktree beside the list", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable())

		harness.Press("space", "e")

		require.NotEmpty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Tmux().Respawns()[0], "'/work/telescope'")
		assert.Equal(t, "/work/telescope", harness.Tmux().RespawnDirs()[0])
	})

	t.Run("on an engine elsewhere the edit key opens the worktree there, through its ssh host", func(t *testing.T) {
		harness := openAgentsWith(t, paneltest.Params{Settings: remoteEditSettings(t, "nvim")}, newLiveWorkflow("w1", "backend"), newEditable())

		harness.Press("space", "e")

		require.NotEmpty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Tmux().Respawns()[0], "-t 'komrad-remote'")
		assert.Empty(t, harness.Tmux().RespawnDirs()[0], "the pane is not rooted at a directory this machine lacks")
	})

	t.Run("on an engine elsewhere an editor that cannot reach it opens nothing", func(t *testing.T) {
		harness := openAgentsWith(t, paneltest.Params{Settings: remoteEditSettings(t, "subl")}, newLiveWorkflow("w1", "backend"), newEditable())

		harness.Press("space", "e")

		assert.Empty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Frame(), "could not edit telescope")
	})

	t.Run("closing the editor mails the agent", func(t *testing.T) {
		tests := []struct {
			name  string
			close func(*paneltest.Panel)
		}{
			{
				name:  "the key again",
				close: func(h *paneltest.Panel) { h.Press("space", "e") },
			},
			{
				name: "the editor quitting",
				close: func(h *paneltest.Panel) {
					editors := h.Tmux().RespawnPanes()
					h.Tmux().SetPaneDead(editors[len(editors)-1])
					h.Advance(time.Second)
				},
			},
			{
				name:  "leaving the agents list",
				close: func(h *paneltest.Panel) { h.Press("shift+tab", "shift+tab") },
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable())
				said := recordMail(harness)
				harness.Press("space", "e")
				require.Empty(t, *said)

				tt.close(harness)

				require.Len(t, *said, 1)
				assert.Equal(t, "a1", (*said)[0].GetTo())
				assert.Contains(t, (*said)[0].GetBody(), "had your worktree open in my editor")
			})
		}
	})

	t.Run("a task's edit opens the worktree its writer was provisioned, not a newer agent's own directory", func(t *testing.T) {
		writer := newEditable()
		task := newTaskOf(writer)
		adopter := newThinking()
		adopter.Id = "a2"
		adopter.Name = "adopter"
		adopter.PaneId = "%8"
		adopter.Cwd = "/work/repo"
		adopter.CreatedAt = "2026-09-20T13:00:00Z"
		adopter.ProvisionDisabled = true
		adopter.TaskId = writer.TaskId
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), writer, adopter, task)

		harness.Press("space", "e")

		require.NotEmpty(t, harness.Tmux().RespawnDirs())
		assert.Equal(t, "/work/telescope", harness.Tmux().RespawnDirs()[0])
	})

	t.Run("moving the cursor keeps the editor on the stage", func(t *testing.T) {
		other := newEditable()
		other.Id, other.Name, other.PaneId = "a2", "comet", "%10"
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(), other)
		moveToAgentRow(t, harness, "telescope")
		harness.Press("space", "e")
		respawns, swaps := len(harness.Tmux().Respawns()), harness.Tmux().Swaps()

		harness.Press("j")
		harness.Advance(time.Second)

		assert.Len(t, harness.Tmux().Respawns(), respawns)
		assert.Equal(t, swaps, harness.Tmux().Swaps())
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceSendMailProcedure))
	})

	t.Run("an agent that may be writing opens no editor", func(t *testing.T) {
		for _, state := range []agentv1.AgentState{agentv1.AgentState_AGENT_STATE_THINKING, agentv1.AgentState_AGENT_STATE_UNCERTAIN} {
			t.Run(state.String(), func(t *testing.T) {
				agent := newEditable()
				agent.State = state
				harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

				harness.Press("space", "e")

				assert.Empty(t, harness.Tmux().Respawns())
				assert.Contains(t, harness.Frame(), "could not edit telescope")
			})
		}
	})

	t.Run("a dormant agent's worktree opens whatever it stopped in", func(t *testing.T) {
		agent := newEditable()
		agent.Alive = xptr.Ptr(false)
		agent.State = agentv1.AgentState_AGENT_STATE_THINKING
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

		harness.Press("space", "e")

		assert.NotEmpty(t, harness.Tmux().Respawns())
	})
}

// TestAgentsOrchestrator covers the agent that stands behind a workflow's row.
func TestAgentsOrchestrator(t *testing.T) {
	dormant := newOrchestrator()
	dormant.Alive = nil
	dormant.State = agentv1.AgentState_AGENT_STATE_IDLE
	failed := newOrchestrator()
	failed.Alive = nil
	failed.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	tests := []struct {
		name       string
		rows       []proto.Message
		want       string
		wantZoomed bool
	}{
		{
			name:       "a workflow whose orchestrator failed to start says so",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), failed},
			want:       "setup failed",
			wantZoomed: true,
		},
		{
			name:       "a workflow whose orchestrator stopped says how to bring it back",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), dormant},
			want:       "dormant · press enter to restore",
			wantZoomed: true,
		},
		{
			name:       "a workflow that never had one renders its shared context",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend")},
			want:       "no shared context set",
			wantZoomed: true,
		},
		{
			name: "a workflow whose orchestrator is running stages its pane",
			rows: []proto.Message{newLiveWorkflow("w1", "backend"), newOrchestrator()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, tt.rows...)
			if !tt.wantZoomed {
				harness.Advance(time.Second)
			}

			assert.Equal(t, tt.wantZoomed, harness.Tmux().IsZoomed())
			if tt.want != "" {
				assert.Contains(t, harness.Frame(), tt.want)
			}
		})
	}

	t.Run("a restart relaunches the orchestrator with the note", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newOrchestrator())
		harness.Answer(agentv1connect.AgentServiceRestartAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.RestartAgentResponse{Agent: newOrchestrator()}), nil
		})

		harness.Press(slices.Concat([]string{"space", "o"}, spellKeys("start fresh"), []string{"enter"})...)

		harness.AssertSentTo(agentv1connect.AgentServiceRestartAgentProcedure, &agentv1.RestartAgentRequest{
			Id:      "o1",
			Note:    "start fresh",
			Harness: "claude",
		})
	})

	t.Run("a running orchestrator whose frame reads as no harness is staged at once", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{HarnessFrames: &harnessFrameFake{}})
		answerWorktreeReads(harness)
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newOrchestrator()))

		assert.Equal(t, "%9", harness.Tmux().GuestPane())
	})

	t.Run("an orchestrator setting up is staged once it is running", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{HarnessFrames: &harnessFrameFake{}})
		answerWorktreeReads(harness)
		settingUp := newOrchestrator()
		settingUp.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), settingUp))
		harness.Advance(time.Second)
		require.Empty(t, harness.Tmux().GuestPane())

		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newOrchestrator()))

		assert.Equal(t, "%9", harness.Tmux().GuestPane())
	})

	t.Run("open on a workflow starts its orchestrator", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))

		harness.Press("enter")

		harness.AssertSent(paneltest.Request{
			Procedure: agentv1connect.AgentServiceStartOrchestratorProcedure,
			Msg:       &agentv1.StartOrchestratorRequest{WorkflowId: "w1"},
		})
	})

	t.Run("moving onto a workflow starts nothing", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newAgent("a1", "telescope", "w1"))

		harness.Press("k")

		require.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))
		harness.AssertSentTo(agentv1connect.AgentServiceStartOrchestratorProcedure)
	})

	t.Run("a flat list renders no row for the orchestrator it folded away", func(t *testing.T) {
		telescope := newAgentAt("telescope", "w1", 1)
		harness := openAgents(t,
			newLiveWorkflow("w1", "backend"),
			newOrchestrator(),
			telescope,
			newTaskOf(telescope),
			newReviewer("a2", "reviewer", "telescope"),
		)

		harness.Press("f", "shift+tab", "j")

		assert.Equal(t, []string{"telescope", "  └ " + reviewerMark + "reviewer"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("a workflow made in the panel comes with its orchestrator", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))
		answerWorkflowCreate(harness)

		harness.Press("space", "w", "a", "p", "i", "enter")

		harness.AssertSentWith(ignoringEventPrompts,
			paneltest.Request{
				Procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
				Msg: &workflowv1.CreateWorkflowRequest{
					Name:     "api",
					Worktree: &workflowv1.WorktreeConfig{Enabled: true},
				},
			},
			paneltest.Request{
				Procedure: agentv1connect.AgentServiceStartOrchestratorProcedure,
				Msg:       &agentv1.StartOrchestratorRequest{WorkflowId: "w2"},
			},
		)
	})

	t.Run("a workflow stands when its orchestrator will not start", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))
		answerWorkflowCreate(harness)
		harness.Refuse(agentv1connect.AgentServiceStartOrchestratorProcedure, connect.NewError(connect.CodePermissionDenied, errors.New("the person refused the directory")))

		harness.Press("space", "w", "a", "p", "i", "enter")
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newLiveWorkflow("w2", "api")))

		frame := harness.Frame()
		assert.NotContains(t, frame, "Could not create the workflow")
		assert.Equal(t, "api", paneltest.HoveredRow(frame), "the cursor lands on the workflow it made")
	})
}

// TestAgentsReviewers covers what a writer and its reviewers allow and say about each other.
func TestAgentsReviewers(t *testing.T) {
	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "one task with one agent",
			press: []string{"space", "a", "d"},
			want:  `Delete task "task-fix-gutter"?`,
		},
		{
			name:  "a task whose writer has reviewers",
			press: []string{"j", "space", "a", "d"},
			want:  `Delete task "task-writer"?`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildReviewedTasks()...)

			harness.Press(tt.press...)

			assert.Contains(t, readFormProse(harness.Frame()), tt.want)
		})
	}

	t.Run("a task offers its writer's worktree a rebase", func(t *testing.T) {
		rows := buildReviewedTasks()
		rows[0] = &workflowv1.Workflow{
			Id:       "w1",
			Name:     "komrad",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		}
		writer, _ := rows[3].(*agentv1.Agent)
		writer.Cwd = "/tmp/tree"
		writerTask, _ := rows[6].(*taskv1.Task)
		writerTask.Cwd = xptr.Ptr("/tmp/tree")
		harness := openAgents(t, rows...)

		harness.Press("j", "space", "g")

		assert.NotRegexp(t, readOffEntry("rebase with base", "r"), harness.RawFrame())
	})
}

// TestAgentsWatchedEdits covers edits the watch delivers reaching the hovered row's record and document.
func TestAgentsWatchedEdits(t *testing.T) {
	t.Run("a workflow context edited elsewhere is rendered without the cursor moving", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "api")
		workflow.Context = "old notes"
		harness := openAgents(t, workflow)
		require.Contains(t, harness.Frame(), "old notes")

		harness.Watch(paneltest.Puts(&workflowv1.Workflow{
			Id:      "w1",
			Name:    "api",
			Context: "new notes",
		}))

		frame := harness.Frame()
		assert.Contains(t, frame, "new notes")
		assert.NotContains(t, frame, "old notes")
	})

	t.Run("a pull request for a branch nobody is on waits for the task that arrives on it", func(t *testing.T) {
		agent, task := newBranchAgent("a1", "api", "agent/x")
		harness := openAgentsWith(t, paneltest.Params{DetailsHeight: 14},
			newLiveWorkflow("w1", "api"),
			agent,
			task,
			&pullrequestv1.Repository{Url: repositoryURL},
		)
		harness.Press(widenListPresses...)

		harness.Watch(paneltest.Puts(&pullrequestv1.PullRequest{
			Id:                "pr-1",
			RepositoryUrl:     repositoryURL,
			HeadRepositoryUrl: repositoryURL,
			HeadBranch:        "agent/other",
			Number:            7,
			State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
			OpenedAt:          "2026-09-19T12:00:00Z",
		}, &pullrequestv1.PullRequest{
			Id:                "pr-0",
			RepositoryUrl:     repositoryURL,
			HeadRepositoryUrl: repositoryURL,
			HeadBranch:        "agent/other",
			Number:            5,
			State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
			OpenedAt:          "2026-09-18T12:00:00Z",
			ClosedAt:          xptr.Ptr("2026-09-18T13:00:00Z"),
		}))
		require.Negative(t, paneltest.RecordLine(harness.Frame(), "pull requests"), "the row on another branch is untouched")

		arrived, arrivedTask := newBranchAgent("a2", "web", "agent/other")
		arrived.CreatedAt = "2026-09-20T12:00:00Z"
		harness.Watch(paneltest.Puts(arrived, arrivedTask))
		harness.Press("k")

		frame := harness.Frame()
		require.Equal(t, "├─ task-a2", paneltest.HoveredRow(frame))
		assert.Equal(t, "#5 closed", paneltest.RecordField(frame, "pull requests"))
	})

	t.Run("a pull request's title and author reach the record of the task on its branch", func(t *testing.T) {
		agent, task := newBranchAgent("a1", "api", "agent/x")
		harness := openAgentsWith(t, paneltest.Params{DetailsHeight: 14},
			newLiveWorkflow("w1", "api"),
			agent,
			task,
			&pullrequestv1.Repository{Url: repositoryURL},
		)
		harness.Press(widenListPresses...)

		harness.Watch(paneltest.Puts(&pullrequestv1.PullRequest{
			Id:                "pr-1",
			RepositoryUrl:     repositoryURL,
			HeadRepositoryUrl: repositoryURL,
			HeadBranch:        "agent/x",
			Number:            7,
			Title:             "Pull more pull request info",
			Author:            "octocat",
			State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
			OpenedAt:          "2026-09-19T12:00:00Z",
		}))

		frame := harness.Frame()
		assert.Equal(t, "#7 Pull more pull request info", paneltest.RecordField(frame, "pull request"))
		assert.Equal(t, "@octocat", paneltest.RecordField(frame, "author"))
	})
}

// TestAgentsSelection covers choosing several tasks and acting on them at once.
func TestAgentsSelection(t *testing.T) {
	tests := []struct {
		name      string
		press     []string
		wantTasks []string
	}{
		{
			name:      "with nothing selected the archive key takes the hovered task",
			press:     []string{},
			wantTasks: []string{"task-auth"},
		},
		{
			name:      "the select key takes in the agent under the cursor",
			press:     []string{"j", "v"},
			wantTasks: []string{"task-api"},
		},
		{
			name:      "and drops one already selected",
			press:     []string{"v", "j", "v", "v"},
			wantTasks: []string{"task-auth"},
		},
		{
			name:      "a header takes its whole folder less the hidden task with no row",
			press:     []string{"k", "v"},
			wantTasks: []string{"task-auth", "task-api", "task-db"},
		},
		{
			name:      "a partly selected folder is filled in rather than emptied",
			press:     []string{"j", "v", "k", "k", "v"},
			wantTasks: []string{"task-auth", "task-api", "task-db"},
		},
		{
			name:      "a wholly selected folder drops and leaves what is outside it",
			press:     []string{"k", "v", "G", "v", "g", "g", "v", "G"},
			wantTasks: []string{"task-ui"},
		},
		{
			name:      "the range key from a list at rest takes the agent under the cursor",
			press:     []string{"j", "j", "V"},
			wantTasks: []string{"task-db"},
		},
		{
			name:      "and on a header takes the folder",
			press:     []string{"k", "V"},
			wantTasks: []string{"task-auth", "task-api", "task-db"},
		},
		{
			name:      "a header takes no range once the mode is up",
			press:     []string{"j", "j", "v", "g", "g", "V", "j", "j", "j"},
			wantTasks: []string{"task-db"},
		},
		{
			name:      "a selection above reaches down to the cursor",
			press:     []string{"v", "j", "j", "V"},
			wantTasks: []string{"task-auth", "task-api", "task-db"},
		},
		{
			name:      "a selection below reaches up to it",
			press:     []string{"j", "j", "v", "k", "k", "V"},
			wantTasks: []string{"task-auth", "task-api", "task-db"},
		},
		{
			name:      "a workflow header inside the range is passed over",
			press:     []string{"j", "j", "v", "G", "V"},
			wantTasks: []string{"task-db", "task-ui"},
		},
		{
			name:      "a cursor as far from a selection either side takes the one above",
			press:     []string{"v", "j", "j", "v", "k", "V"},
			wantTasks: []string{"task-auth", "task-api", "task-db"},
		},
		{
			name:      "a selection spanning folders archives every task in it",
			press:     []string{"v", "G", "v", "g", "g", "j"},
			wantTasks: []string{"task-auth", "task-ui"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, withTasks(buildSelectable())...)
			harness.Press(tt.press...)

			harness.Press("space", "a")
			harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure)
			harness.Press("enter")

			harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, readArchiveTaskRequests(tt.wantTasks)...)
		})
	}

	t.Run("a folder with no rows under it opens no selection", func(t *testing.T) {
		harness := openAgents(t, append(buildSelectable(), newLiveWorkflow("w3", "chores"))...)

		harness.Press("j", "j", "j")
		require.Equal(t, "chores", paneltest.HoveredRow(harness.Frame()))

		harness.Press("v")
		assert.NotContains(t, paneltest.Legend(harness.Frame()), "selected")
	})

	t.Run("keys with nothing to say about a set stand down and escape clears it", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.Press("v")

		harness.Press("r")
		assert.NotContains(t, harness.Frame(), "enter submit", "rename does not open its form")

		harness.Press("j")
		assert.Equal(t, "├─ api", paneltest.HoveredRow(harness.Frame()), "the cursor still moves")

		harness.Press("esc")
		assert.NotContains(t, paneltest.Legend(harness.Frame()), "selected")
	})

	t.Run("an agent gone from the listing drops out of the selection", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)
		harness.Press("v", "G", "v")

		ui := newAgentAt("ui", "w2", 1)
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w2", "frontend"), ui, newTaskOf(ui)))
		harness.Press("space", "a", "enter")

		harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, readArchiveTaskRequests([]string{"task-ui"})...)
	})

	t.Run("so does one the hidden toggle puts away", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)
		harness.Press("space", "h", "H", "v", "j", "j", "j", "v", "space", "h", "H")

		harness.Press("space", "a", "enter")

		harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, readArchiveTaskRequests([]string{"task-auth"})...)
	})
}

func readArchiveTaskRequests(ids []string) []proto.Message {
	return xslice.Map(ids, func(id string) proto.Message { return &taskv1.ArchiveTaskRequest{Task: id} })
}

const (
	hiddenMark    = " \uf4c5"
	reviewerMark  = "\uf442 "
	heldAt        = "2026-09-20T12:00:00Z"
	repositoryURL = "https://github.com/harrisoncramer/komrad"
)

func openAgents(t *testing.T, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	return openAgentsWith(t, paneltest.Params{}, rows...)
}

func openAgentsWith(t *testing.T, params paneltest.Params, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	answerWorktreeReads(harness)
	harness.Watch(paneltest.Snapshot(rows...))

	return harness
}

func answerWorktreeReads(harness *paneltest.Panel) {
	harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*worktreev1.GetWorktreeRequest)

		return connect.NewResponse(&worktreev1.GetWorktreeResponse{Worktree: &worktreev1.Worktree{Dir: read.GetDir()}}), nil
	})
}

func buildCursorable() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		newAgentAt("auth", "w1", 3),
		newAgentAt("api", "w1", 2),
		newAgentAt("db", "w1", 1),
		newAgentAt("ui", "w2", 0),
	}
}

func buildReviewedTasks() []proto.Message {
	fixGutter := newAgentAt("fix-gutter", "w1", 2)
	writer := newAgentAt("writer", "w1", 1)
	reviewer := newReviewer("a3", "reviewer", "writer")
	secondReviewer := newReviewer("a4", "second-reviewer", "writer")
	secondReviewer.CreatedAt = "2026-09-20T12:09:00Z"
	writerTask := newTaskOf(writer)

	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		fixGutter,
		newTaskOf(fixGutter),
		writer,
		reviewer,
		secondReviewer,
		writerTask,
	}
}

func buildSortable() []proto.Message {
	newSortable := func(name, workflowID string, created, changed int) *agentv1.Agent {
		agent := newAgentAt(name, workflowID, created)
		agent.StateChangedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", changed)

		return agent
	}
	charlie := newSortable("charlie", "w1", 25, 5)
	charlie.TaskId = xptr.Ptr("task-alpha")
	charlie.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
	bob := newSortable("bob", "w1", 5, 40)
	bob.TaskId = xptr.Ptr("task-alpha")
	bob.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
	zulu := newSortable("zulu", "w1", 20, 30)
	zulu.Name = "Zulu"
	alpha := newSortable("alpha", "w1", 10, 20)

	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		newLiveWorkflow("w2", "gadgets"),
		newSortable("delta", "w1", 30, 10),
		charlie,
		zulu,
		alpha,
		newTaskOf(alpha),
		bob,
		newSortable("echo", "w2", 0, 50),
	}
}

func hideTaskOf(rows []proto.Message, agentID string) []proto.Message {
	var taskID string
	for _, row := range rows {
		if agent, isAgent := row.(*agentv1.Agent); isAgent && agent.GetId() == agentID {
			taskID = agent.GetTaskId()
		}
	}
	for _, row := range rows {
		if task, isTask := row.(*taskv1.Task); isTask && task.GetId() == taskID {
			task.Hidden = true
		}
	}

	return rows
}

func newAgent(id, name, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       name,
		WorkflowId: workflowID,
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
	}
}

// newTaskOf makes an agent the writer of a task of its own and returns the task, for a test to give its checkout.
func newTaskOf(agent *agentv1.Agent) *taskv1.Task {
	agent.TaskId = xptr.Ptr("task-" + agent.GetId())
	agent.Role = agentv1.AgentRole_AGENT_ROLE_WRITER

	return &taskv1.Task{
		Id:         agent.GetTaskId(),
		WorkflowId: agent.GetWorkflowId(),
		Slug:       "task-" + agent.GetId(),
	}
}

func withTasks(rows []proto.Message) []proto.Message {
	var placed []proto.Message
	for _, row := range rows {
		placed = append(placed, row)
		if agent, ok := row.(*agentv1.Agent); ok {
			placed = append(placed, newTaskOf(agent))
		}
	}

	return placed
}

func newAgentAt(name, workflowID string, minute int) *agentv1.Agent {
	agent := newAgent(name, name, workflowID)
	agent.CreatedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", minute)

	return agent
}

func newAgentOnHiddenTask(id, name string) []proto.Message {
	agent := newAgent(id, name, "w1")
	task := newTaskOf(agent)
	task.Hidden = true

	return []proto.Message{agent, task}
}

func newReviewer(id, name, writerID string) *agentv1.Agent {
	agent := newAgent(id, name, "w1")
	agent.TaskId = xptr.Ptr("task-" + writerID)
	agent.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

	return agent
}

func newOrchestrator() *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "o1",
		Name:       "orchestrator",
		WorkflowId: "w1",
		PaneId:     "%9",
		Harness:    "claude",
		Alive:      xptr.Ptr(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
		Role:       agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
	}
}

func findRawLine(frame string, pattern *regexp.Regexp) string {
	for _, line := range paneltest.SplitLines(frame) {
		if pattern.MatchString(ansi.Strip(line)) {
			return line
		}
	}

	return ""
}

func openCopyRecord(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{})
	answerWorktreeReads(harness)
	harness.Watch(paneltest.Snapshot(buildRecorded()...))

	return harness
}

func openAgentRecord(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := openAgents(t, buildRecorded()...)
	harness.Press("pick:record")

	return harness
}

func openWorktreeRecord(t *testing.T, params paneltest.Params) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
	harness.Watch(paneltest.Snapshot(buildRecordedTask()...))
	harness.Press("pick:record")

	return harness
}

func readSearch(frame string) string {
	for _, line := range paneltest.BelowViews(frame) {
		prompt := strings.TrimSpace(ansi.Truncate(line, paneltest.ListWidth(frame), ""))
		if strings.HasPrefix(prompt, "/") || strings.HasPrefix(prompt, "▽") {
			return prompt
		}
	}

	return ""
}

func findRecordCursor(frame string) int {
	return slices.IndexFunc(paneltest.Record(frame), func(line string) bool {
		return strings.HasPrefix(line, "› ")
	})
}

func countCursors(frame string) int {
	return strings.Count(strings.Join(paneltest.BelowViews(frame), "\n"), "›")
}

func isBlank(line string) bool {
	return strings.TrimSpace(line) == ""
}

func buildWorktreeRead() *worktreev1.GetWorktreeResponse {
	return &worktreev1.GetWorktreeResponse{
		Worktree: &worktreev1.Worktree{
			Dir:        "/repo",
			Readable:   true,
			Branch:     "agent/x",
			Occupants:  []*worktreev1.WorktreeAgent{{Id: "a2", Name: "reader"}},
			SizeBytes:  xptr.Ptr[int64](1200 * 1024 * 1024),
			MeasuredAt: xptr.Ptr(time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)),
		},
		Modified:           []string{"a.go", "b.go"},
		Untracked:          []string{"notes.md"},
		BaseRef:            "origin/main",
		Ahead:              3,
		Behind:             12,
		Pushed:             true,
		Unpushed:           2,
		Unpulled:           1,
		LandedCommits:      1,
		SequenceInProgress: true,
		Commits: []*worktreev1.Commit{
			{
				Sha:         "87a3bcc98c47",
				Subject:     "fix(view): drop the column",
				CommittedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			},
			{
				Sha:         "1234567890ab",
				Subject:     "feat(ui): a thing",
				CommittedAt: time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339),
			},
		},
	}
}

func readRecordTag(frame string) string {
	for _, line := range paneltest.SplitLines(frame) {
		if strings.HasPrefix(line, "─") {
			tag := strings.TrimPrefix(strings.Trim(strings.TrimRight(line, " │┤┼"), "─ "), "Info")

			return strings.Trim(tag, "─ ")
		}
	}

	return ""
}

func answerWith(read *worktreev1.GetWorktreeResponse) paneltest.AnswerFunc {
	return func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(read), nil
	}
}

func refuseWith(code connect.Code, reason string) paneltest.AnswerFunc {
	return func(connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(code, errors.New(reason))
	}
}

func buildWorktreeReads(count int) []proto.Message {
	return slices.Repeat([]proto.Message{&worktreev1.GetWorktreeRequest{Dir: "/repo"}}, count)
}

func buildRecorded() []proto.Message {
	agent := newAgentAt("auth", "w1", 0)
	agent.Id = "a1"
	agent.Cwd = "/repo"
	agent.SessionId = "abc"

	return []proto.Message{
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		agent,
	}
}

// buildRecordedWith is buildRecorded with its agent changed by edit.
func buildRecordedWith(edit func(agent *agentv1.Agent)) []proto.Message {
	rows := buildRecorded()
	edit(rows[1].(*agentv1.Agent))

	return rows
}

// buildRecordedTask is buildRecorded with its agent on a task whose checkout is /repo.
func buildRecordedTask() []proto.Message {
	rows := buildRecorded()
	task := newTaskOf(rows[1].(*agentv1.Agent))
	task.Cwd = xptr.Ptr("/repo")
	task.Branch = xptr.Ptr("agent/auth")
	task.CreatedAt = "2026-09-20T12:00:00Z"

	return append(rows, task)
}

func newThinking() *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "a1",
		Name:       "telescope",
		WorkflowId: "w1",
		PaneId:     "%9",
		Alive:      xptr.Ptr(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
	}
}

func recordMail(harness *paneltest.Panel) *[]*agentv1.SendMailRequest {
	var said []*agentv1.SendMailRequest
	harness.Answer(agentv1connect.AgentServiceSendMailProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		mail, _ := req.Any().(*agentv1.SendMailRequest)
		said = append(said, mail)

		return connect.NewResponse(&agentv1.SendMailResponse{}), nil
	})

	return &said
}

func answerWorkflowCreate(harness *paneltest.Panel) {
	harness.Answer(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}), nil
	})
}

func newBranchAgent(id, name, branch string) (*agentv1.Agent, *taskv1.Task) {
	agent := newAgent(id, name, "w1")
	task := newTaskOf(agent)
	task.RepositoryUrl = xptr.Ptr(repositoryURL)
	task.Branch = xptr.Ptr(branch)

	return agent, task
}

func buildSelectable() []proto.Message {
	return slices.Concat([]proto.Message{
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		newAgentAt("auth", "w1", 5),
		newAgentAt("api", "w1", 4),
		newAgentAt("db", "w1", 3),
		newAgentAt("ui", "w2", 1),
	}, newAgentOnHiddenTask("sweep", "sweep"))
}

func newEditable() *agentv1.Agent {
	agent := newThinking()
	agent.State = agentv1.AgentState_AGENT_STATE_IDLE
	agent.Cwd = "/work/telescope"

	return agent
}

// TestAgentsRowShowsTheTool covers the tool an agent reported, drawn after its name while it works.
func TestAgentsRowShowsTheTool(t *testing.T) {
	agent := newThinking()
	agent.State = agentv1.AgentState_AGENT_STATE_THINKING
	agent.ReportedReason = "WebFetch"
	harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

	row := findRowWith(t, harness.Frame(), "telescope")
	assert.Contains(t, row, "WebFetch")
}
