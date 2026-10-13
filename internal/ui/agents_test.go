package ui_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui"
	"komrad/internal/ui/paint"
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
			want: []string{"komrad", "├─ task-fix-gutter", "└─ task-writer", "   ├─ second-reviewer", "   └─ reviewer"},
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
			want: []string{"komrad", "├─ fix-gutter", "└─ task-writer", "   ├─ second-reviewer", "   └─ reviewer"},
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

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("a row opens at the gutter and every kind of row opens its name in one column", func(t *testing.T) {
		harness := openAgents(t,
			&workflowv1.Workflow{
				Id:       "w1",
				Name:     "komrad",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
			newAgentAt("worker", "w1", 1),
			newAgentAt("other", "w1", 0),
		)

		rows := xslice.Map(readListLines(harness.readFrame()), func(line listLine) string { return line.sidebar })
		require.Len(t, rows, 3)
		other := rows[2]
		assert.Len(t, other[:len(other)-len(strings.TrimLeft(other, " "))], 2, "an agent opens on its checkout column after the focus cell")
		for _, row := range rows[1:] {
			assert.Equal(t, findNameColumn(rows[0]), findNameColumn(row), "the name or the tree before it opens in one column whatever the row is")
		}
	})

	t.Run("a branch renders nothing on the row", func(t *testing.T) {
		plainAgent := newAgent("a1", "worker", "w1")
		branchedAgent := newAgent("a1", "worker", "w1")
		branch := newTaskOf(branchedAgent)
		branch.Branch = xptr.Ptr("agent/a1")

		plain := openAgents(t, newLiveWorkflow("w1", "backend"), plainAgent, newTaskOf(plainAgent))
		branched := openAgents(t, newLiveWorkflow("w1", "backend"), branchedAgent, branch)

		assert.Equal(t, readListRows(plain.readFrame()), readListRows(branched.readFrame()))
	})

	t.Run("an empty panel welcomes a newcomer and says where to start", func(t *testing.T) {
		harness := openAgents(t)

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, frame, "Welcome, komrad!")
		assert.Contains(t, frame, "Press 'space' then 'w' to set up your first workflow")
		assert.Empty(t, readListRows(frame))
		ui.RequireGolden(t, "agents-welcome", harness.readRawFrame())
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
		harness.press("j", "j")
		require.Equal(t, "└─ task-a3", readHoveredRow(harness.readFrame()))

		harness.press("space", "h", "H")

		frame := harness.readFrame()
		assert.Equal(t, []string{"backend", "├─ task-a2" + hiddenMark, "└─ task-a1", "frontend", "└─ task-a3"}, readListRows(frame))
		assert.Equal(t, "└─ task-a3", readHoveredRow(frame))
	})

	t.Run("a hidden task comes back with every agent on it", func(t *testing.T) {
		harness := openAgents(t, hideTaskOf(buildReviewedTasks(), "writer")...)

		harness.press("k", "space", "h", "H")

		assert.Equal(t, []string{"komrad", "├─ task-fix-gutter", "└─ task-writer" + hiddenMark, "   ├─ second-reviewer", "   └─ reviewer"}, readListRows(harness.readFrame()))
	})

	t.Run("the hide entry hides the hovered task", func(t *testing.T) {
		harness := openAgents(t, rows...)

		harness.press("space", "h", "h")

		harness.assertSentTo(taskv1connect.TaskServiceSetTaskHiddenProcedure, &taskv1.SetTaskHiddenRequest{Task: "task-a1", Hidden: true})
	})

	t.Run("the hide entry unhides a revealed task", func(t *testing.T) {
		harness := openAgents(t, rows...)

		harness.press("space", "h", "H", "k", "space", "h", "h")

		harness.assertSentTo(taskv1connect.TaskServiceSetTaskHiddenProcedure, &taskv1.SetTaskHiddenRequest{Task: "task-a2", Hidden: false})
	})

	t.Run("a selection covering a whole task hides it", func(t *testing.T) {
		harness := openAgents(t, buildReviewedTasks()...)

		harness.press("v", "space", "h", "h")

		harness.assertSentTo(taskv1connect.TaskServiceSetTaskHiddenProcedure, &taskv1.SetTaskHiddenRequest{Task: "task-fix-gutter", Hidden: true})
	})

	t.Run("a selection reaching part of a task holds hide gray", func(t *testing.T) {
		harness := openAgents(t, buildReviewedTasks()...)

		harness.press("v", "j", "j", "j", "v", "space", "h")

		assert.Regexp(t, readOffEntry("hide", "h"), harness.readRawFrame())
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

			harness.press(append(tt.press, "space", "h")...)

			frame := harness.readFrame()
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
			want: []string{"gadgets", "└─ echo", "komrad", "├─ delta", "├─ Zulu", "└─ task-alpha", "   ├─ charlie", "   └─ bob"},
		},
		{
			name:  "created up is the oldest task first",
			press: []string{"f", "k"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ task-alpha", "│  ├─ bob", "│  └─ charlie", "├─ Zulu", "└─ delta"},
		},
		{
			name:  "activity down is the task whose writer changed state last first",
			press: []string{"f", "tab", "j"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ task-alpha", "│  ├─ bob", "│  └─ charlie", "└─ delta"},
		},
		{
			name:  "name up sorts a name in capitals as its lowercase",
			press: []string{"f", "tab", "tab", "k"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ task-alpha", "│  ├─ bob", "│  └─ charlie", "├─ delta", "└─ Zulu"},
		},
		{
			name:  "name down",
			press: []string{"f", "tab", "tab", "j"},
			want:  []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ delta", "└─ task-alpha", "   ├─ charlie", "   └─ bob"},
		},
		{
			name:  "a flat list drops the headers and walks every workflow's tasks together",
			press: []string{"f", "shift+tab", "j"},
			want:  []string{"delta", "Zulu", "alpha", "  └ charlie", "  └ bob", "echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildSortable()...)

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("the cursor keeps its row as the tree is reordered", func(t *testing.T) {
		harness := openAgents(t, buildSortable()...)
		harness.press("j", "j", "j", "j")
		require.Equal(t, "└─ task-alpha", readHoveredRow(harness.readFrame()))

		harness.press("f", "tab", "esc")
		assert.Equal(t, "└─ task-alpha", readHoveredRow(harness.readFrame()), "walking to a tab reorders nothing")

		harness.press("f", "tab", "j", "esc")
		assert.Equal(t, "├─ task-alpha", readHoveredRow(harness.readFrame()), "activity down")

		harness.press("f", "tab", "k", "esc")
		assert.Equal(t, "├─ task-alpha", readHoveredRow(harness.readFrame()), "name up")
	})

	t.Run("the cursor keeps its agent across the flip to a flat list and back", func(t *testing.T) {
		harness := openAgents(t, buildSortable()...)
		require.Equal(t, "└─ echo", readHoveredRow(harness.readFrame()))

		harness.press("f", "shift+tab", "j", "esc")
		assert.Equal(t, "echo", readHoveredRow(harness.readFrame()), "the flat list")

		harness.press("f", "shift+tab", "k", "esc")
		assert.Equal(t, "└─ echo", readHoveredRow(harness.readFrame()), "the tree again")
	})
}

// TestAgentsLongList covers a list with more rows than its pane.
func TestAgentsLongList(t *testing.T) {
	rows := []proto.Message{newLiveWorkflow("w1", "komrad")}
	for i := range 40 {
		rows = append(rows, newAgentAt(fmt.Sprintf("agent-%02d", i), "w1", 39-i))
	}

	tests := []struct {
		name       string
		press      []string
		want       string
		wantMore   []string
		wantAbsent []string
	}{
		{
			name:       "at the top only what is below is counted",
			want:       "├─ agent-00",
			wantMore:   []string{"↓"},
			wantAbsent: []string{"↑"},
		},
		{
			name:     "in the middle both ends are",
			press:    slices.Repeat([]string{"j"}, 20),
			want:     "├─ agent-20",
			wantMore: []string{"↑", "↓"},
		},
		{
			name:       "at the bottom only what is above is",
			press:      []string{"G"},
			want:       "└─ agent-39",
			wantMore:   []string{"↑"},
			wantAbsent: []string{"↓"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{height: 23, detailsHeight: 8})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(rows...))

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Len(t, splitLines(frame), 23)
			assert.Equal(t, tt.want, readHoveredRow(frame), "the row the cursor stands on is inside the window")
			for _, marker := range tt.wantMore {
				assert.Regexp(t, marker+` \d+ more`, frame)
			}
			for _, marker := range tt.wantAbsent {
				assert.NotRegexp(t, marker+` \d+ more`, frame)
			}
		})
	}
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

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readHoveredRow(harness.readFrame()))
		})
	}

	t.Run("the keys do nothing on an empty tree", func(t *testing.T) {
		harness := openAgents(t)

		harness.press("G", "g", "g", "enter")

		assert.Empty(t, readHoveredRow(harness.readFrame()))
		harness.assertSent()
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
		harness.hold(agentv1connect.AgentServiceResumeAgentProcedure)

		harness.press("j", "j")
		require.Equal(t, "└─ old", readHoveredRow(harness.readFrame()))
		harness.press("enter", "k")
		moved := readHoveredRow(harness.readFrame())
		require.NotEqual(t, "  old", moved)

		harness.release(agentv1connect.AgentServiceResumeAgentProcedure)

		assert.Equal(t, moved, readHoveredRow(harness.readFrame()), "the landing leaves the cursor where it was moved")
	})

	t.Run("the open key again over an agent still resuming sends nothing", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)
		harness.hold(agentv1connect.AgentServiceResumeAgentProcedure)

		harness.press("j", "enter", "enter")
		harness.release(agentv1connect.AgentServiceResumeAgentProcedure)

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceResumeAgentProcedure), 1)
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})
	t.Run("a resume says the agent is back up only once its pane decides its state", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.press("j", "enter")
		require.NotContains(t, readOpenMessages(harness), "old is back up", "the write landing says nothing yet")

		settingUp := newRunning()
		settingUp.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), settingUp))
		require.NotContains(t, readOpenMessages(harness), "old is back up", "a CLI still starting is not back up")
		require.Contains(t, harness.readFrame(), "Its CLI is coming up in a new pane.")

		settledBeforeAlive := newRunning()
		settledBeforeAlive.Alive = xptr.Ptr(false)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), settledBeforeAlive))
		require.NotContains(t, readOpenMessages(harness), "old is back up", "an idle row not yet alive is not back up")

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))
		assert.Contains(t, readOpenMessages(harness), "old is back up")
	})

	t.Run("a resume whose pane went before its CLI came up is never said to be back up", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.press("j", "enter")

		settingUp := newRunning()
		settingUp.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), settingUp))
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))

		assert.NotContains(t, readOpenMessages(harness), "old is back up")
	})

	t.Run("a refused resume never says the agent is back up", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		harness.answer(agentv1connect.AgentServiceResumeAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("held"))
		})

		harness.press("j", "enter")
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))

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
			harness.press(tt.press...)

			harness.watch(buildSnapshot(tt.next...))

			frame := harness.readFrame()
			assert.Equal(t, agentsScreen, harness.readScreen())
			assert.Equal(t, tt.want, readHoveredRow(frame))
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

			harness.press(append(tt.press, widenListPresses...)...)

			frame := harness.readFrame()
			for label, value := range tt.want {
				assert.Equal(t, value, readRecordField(frame, label), label)
			}
			for _, label := range tt.wantAbsent {
				assert.Negative(t, findRecordLine(frame, label), label)
			}
		})
	}
}

// TestAgentsRecordFocus covers the focus key handing the keys to the record and back.
func TestAgentsRecordFocus(t *testing.T) {
	t.Run("movement runs down the fields while the list keeps its row", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("pick:record", "j")

		frame := harness.readFrame()
		assert.Regexp(t, `^directory\s+/repo$`, readRecordCursor(frame))
		assert.Contains(t, readLegend(frame), "esc back to the list")

		harness.press("pick:list")

		frame = harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "└─ auth", readHoveredRow(frame), "the list kept its row")

		harness.press("k")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()), "the list has the keys again")
	})

	t.Run("the wheel over the record hands it the keys and over the list hands them back", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		y := findRecordLine(harness.readFrame(), "agent")
		require.GreaterOrEqual(t, y, 0)

		harness.send(tea.MouseWheelMsg{
			X:      1,
			Y:      y,
			Button: tea.MouseWheelDown,
		})

		frame := harness.readFrame()
		assert.Regexp(t, `^directory\s+/repo$`, readRecordCursor(frame))

		harness.send(tea.MouseWheelMsg{
			X:      1,
			Y:      9,
			Button: tea.MouseWheelUp,
		})

		frame = harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "backend", readHoveredRow(frame), "the list has the keys again")
	})

	t.Run("one cursor is rendered at a time", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, 1, countCursors(harness.readFrame()))

		harness.press("pick:record")

		frame := harness.readFrame()
		assert.Equal(t, 1, countCursors(frame))
		assert.NotEmpty(t, readRecordCursor(frame), "the cursor is the record's")
	})

	t.Run("another row's record opens at its top", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("pick:record", "j", "j", "pick:list", "k", "pick:record")

		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()))
	})

	t.Run("the keys that act on a row stand down while the record has them", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("pick:record")
		before := harness.readFrame()
		sent := harness.readRequests()

		harness.press("r", "n", "c")

		assert.Equal(t, before, harness.readFrame())
		harness.assertSent(sent...)
	})

	t.Run("nothing listed gives the record no letter", func(t *testing.T) {
		harness := openAgents(t)

		harness.press("ctrl+space", "j")

		assert.Empty(t, harness.panel.ReadPickLetter("record"))
	})

	t.Run("a record shortened under the cursor holds it on its last field", func(t *testing.T) {
		rows := buildRecorded()
		agent, _ := rows[1].(*agentv1.Agent)
		task := newTaskOf(agent)
		task.Branch = xptr.Ptr("agent/auth")
		rows = append(rows, task)
		harness := openAgents(t, rows...)
		harness.press("pick:record", "G")
		require.Regexp(t, `^task_id\s`, readRecordCursor(harness.readFrame()))

		task.Branch = nil
		harness.watch(buildSnapshot(rows...))

		assert.Regexp(t, `^task_id\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("the row going from under a focused record renders the next from its top", func(t *testing.T) {
		rows := buildRecorded()
		harness := openAgents(t, rows...)
		harness.press("pick:record", "j", "j", "j")

		harness.watch(buildSnapshot(rows[0]))
		require.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()))

		harness.press("j")
		assert.Regexp(t, `^agents\s+0$`, readRecordCursor(harness.readFrame()))
	})
}

// TestAgentsRecordCopy covers the copy key taking the field under the record's cursor.
func TestAgentsRecordCopy(t *testing.T) {
	t.Run("a clipboard that refuses the write says so", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.panel.SetClipboardErr(errors.New("no clipboard utility found"))
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("pick:record", "y")
		harness.fireTimers()
		opened := readOpenMessages(harness)
		require.Contains(t, opened, "Could not copy")
		require.Contains(t, opened, "no clipboard utility found")
		harness.press("ctrl+g")
		harness.fireTimers()
		refused := harness.readFrame()
		require.Contains(t, readEngineLine(refused), "✗ 1")

		harness.panel.SetClipboardErr(nil)
		harness.press("y")
		harness.fireTimers()

		assert.Equal(t, refused, harness.readFrame(), "a copy that worked reports nothing of its own")
	})

	tests := []struct {
		name  string
		open  func(*testing.T) *panelHarness
		press []string
		want  string
	}{
		{
			name: "the whole value is taken rather than the clipped line it is rendered on",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				rows := buildRecorded()
				agent, _ := rows[1].(*agentv1.Agent)
				agent.Flags = strings.Repeat("--append-system-prompt read every file on the branch ", 4)
				harness := newPanelHarness(t, panelHarnessParams{})
				answerWorktreeReads(harness)
				harness.watch(buildSnapshot(rows...))

				return harness
			},
			press: []string{"pick:record", "j", "j"},
			want:  strings.Repeat("--append-system-prompt read every file on the branch ", 4),
		},
		{
			name: "a narrowed record copies from the rows that are left",
			open: func(t *testing.T) *panelHarness {
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

			harness.press(append(tt.press, "y")...)

			assert.Equal(t, []string{tt.want}, harness.panel.ReadCopied())
		})
	}

	t.Run("the field on screen is taken after the row goes from under the record", func(t *testing.T) {
		rows := buildRecorded()
		harness := openCopyRecord(t)
		harness.press("pick:record", "j", "j", "j")

		harness.watch(buildSnapshot(rows[0]))
		harness.press("y")

		assert.Equal(t, []string{"backend"}, harness.panel.ReadCopied())
	})
}

// TestAgentsRecordEmphasis covers what the record says in colour rather than in text.
func TestAgentsRecordEmphasis(t *testing.T) {
	t.Run("a search typed on the list lights its match in the record", func(t *testing.T) {
		harness := openAgents(t, buildRecordedTask()...)

		harness.press("/", "a", "u", "t", "h")

		assert.Contains(t, findRawLine(harness.readRawFrame(), recordBranchLine), paint.Panel.Match().Render("auth"))
	})

	t.Run("a search standing over the record lights the matched run of a value", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "m", "o", "d", "i", "f", "i", "e", "d", "enter")

		assert.Contains(t, findRawLine(harness.readRawFrame(), recordChangeLine), paint.Panel.Match().Render("modified"))
	})

	t.Run("the counts wear the tone of what they count", func(t *testing.T) {
		recorded := buildRecordedTask()
		recorded[len(recorded)-1].(*taskv1.Task).DiffStat = &taskv1.DiffStat{Added: 200, Removed: 100}
		harness := openWorktreeRecord(t, panelHarnessParams{}, recorded...)
		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.readRawFrame()
		for _, want := range []string{
			paint.Panel.Style(paint.ToneSuccess).Render("+200"),
			paint.Panel.Style(paint.ToneDanger).Render("−100"),
		} {
			assert.Contains(t, frame, want)
		}
	})

	t.Run("the hovered value takes the accent only while the record has the keys", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		value := paint.Panel.Style(paint.TonePrimary).Render("—")
		require.NotContains(t, findRawLine(harness.readRawFrame(), recordAgentLine), value)

		harness.press("pick:record")

		lit := findRawLine(harness.readRawFrame(), recordAgentLine)
		assert.Contains(t, lit, value)
		assert.Equal(t, 1, strings.Count(ansi.Strip(lit), "›"))
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
			name:          "a share that rounds down to no field closes it too",
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
			harness := newPanelHarness(t, panelHarnessParams{
				height:        tt.height,
				detailsHeight: tt.detailsHeight,
			})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(buildRecorded()...))

			frame := harness.readFrame()
			assert.Equal(t, tt.want > 0, isRecordRendered(frame))
			assert.Len(t, readRecord(frame), tt.want)
		})
	}

	t.Run("a record shorter than its share pads to it so the list does not reflow", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{detailsHeight: 12})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildRecorded()...))

		agent := readRecord(harness.readFrame())
		harness.press("k")
		workflow := readRecord(harness.readFrame())

		assert.Len(t, agent, 12)
		assert.Len(t, workflow, 12)
		assert.Empty(t, workflow[len(workflow)-1], "the workflow's shorter record ends on padding")
	})

	t.Run("a record longer than its share says how much is left either side of the cursor", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{detailsHeight: 3})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildRecordedTask()...))
		harness.fireTimers()
		require.Equal(t, "  ↓ 7 more", readRecord(harness.readFrame())[2])

		harness.press("pick:record", "G", "pick:list")

		record := readRecord(harness.readFrame())
		assert.Equal(t, "  ↑ 7 more", record[0])
		assert.NotContains(t, record, "  ↓ 7 more")
	})
}

// TestAgentsRecordFocusAndSize covers the record giving up the keys when the pane has no room to render it.
func TestAgentsRecordFocusAndSize(t *testing.T) {
	rows := append(buildRecorded(), newAgentAt("billing", "w1", 1))

	tests := []struct {
		name   string
		params panelHarnessParams
	}{
		{
			name:   "a pane too short to render the record takes no letter",
			params: panelHarnessParams{height: 8},
		},
		{
			name: "a share too small for a field takes no letter",
			params: panelHarnessParams{
				height:        19,
				detailsHeight: 0.05,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, tt.params)
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(rows...))

			harness.press("ctrl+space", "j")

			assert.Empty(t, harness.panel.ReadPickLetter("record"))
		})
	}

	t.Run("a shrink takes the keys back and a grow does not return them", func(t *testing.T) {
		harness := openAgents(t, rows...)
		harness.press("pick:record", "j")
		require.NotEmpty(t, readRecordCursor(harness.readFrame()))

		harness.send(tea.WindowSizeMsg{
			Width:  defaultHarnessWidth,
			Height: 4,
		})
		harness.press("j")
		harness.send(tea.WindowSizeMsg{
			Width:  defaultHarnessWidth,
			Height: defaultHarnessHeight,
		})

		frame := harness.readFrame()
		assert.Equal(t, "└─ auth", readHoveredRow(frame), "the list answered the key pressed while it was small")
		assert.Empty(t, readRecordCursor(frame))

		harness.press("k")
		assert.Equal(t, "├─ billing", readHoveredRow(harness.readFrame()), "the keys are still with the list")

		harness.press("pick:record")
		assert.NotEmpty(t, readRecordCursor(harness.readFrame()), "until the focus key is pressed again")
	})
}

// TestAgentsRecordWorktree covers the task's record carrying what the server read of its directory.
func TestAgentsRecordWorktree(t *testing.T) {
	unreadable := buildWorktreeRead()
	unreadable.Worktree.Readable = false

	tests := []struct {
		name       string
		answer     answerFunc
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
			harness := newPanelHarness(t, panelHarnessParams{})
			harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, tt.answer)
			harness.watch(buildSnapshot(buildRecordedTask()...))

			harness.press(append([]string{"pick:record"}, widenListPresses...)...)

			frame := harness.readFrame()
			for label, value := range tt.want {
				assert.Equal(t, value, readRecordField(frame, label), label)
			}
			for _, label := range tt.wantAbsent {
				assert.Negative(t, findRecordLine(frame, label), label)
			}
		})
	}

	t.Run("a flat list puts the task's checkout on the agent's record", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecordedTask()...))

		harness.press("f", "shift+tab", "j", "esc")
		harness.fireTimers()
		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Equal(t, "agent/auth", readRecordField(frame, "branch"))
		assert.Equal(t, "2 modified, 1 untracked", readRecordField(frame, "changes"))
	})

	t.Run("the standing follows the checkout", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press(widenListPresses...)

		frame := harness.readFrame()
		branch, changes, size, taskID := findRecordLine(frame, "branch"), findRecordLine(frame, "changes"), findRecordLine(frame, "size"), findRecordLine(frame, "task_id")
		require.Positive(t, branch)
		assert.Less(t, branch, changes)
		assert.Less(t, size, taskID, "the standing comes before the ids")
		assert.Negative(t, findRecordLine(frame, "commits"))
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
				harness := newPanelHarness(t, panelHarnessParams{})
				if tt.read == nil {
					harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"))
				} else {
					harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(tt.read))
				}
				harness.watch(buildSnapshot(recorded...))

				harness.press(tt.presses...)
				harness.fireTimers()

				assert.Equal(t, tt.want, readRecordTag(harness.readFrame()))
			})
		}
	})

	t.Run("the focus key reads the directory again", func(t *testing.T) {
		harness := openAgents(t, buildRecordedTask()...)
		harness.fireTimers()
		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(1)...)

		harness.press("pick:record")

		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(2)...)
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
				harness.assertSentTo(loopv1connect.LoopServiceWakeLoopProcedure)

				harness.press(tt.presses...)
				harness.fireTimers()

				harness.assertSentTo(loopv1connect.LoopServiceWakeLoopProcedure, &loopv1.WakeLoopRequest{Loop: loopv1.Loop_LOOP_CWD_SYNC})
			})
		}
	})

	t.Run("a refused reread keeps the last answer beside the refusal", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecordedTask()...))
		harness.fireTimers()
		harness.refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("context deadline exceeded")))

		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Equal(t, "could not read: context deadline exceeded", readRecordField(frame, "worktree"))
		assert.Equal(t, "2 modified, 1 untracked", readRecordField(frame, "changes"))
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
				harness.fireTimers()
				harness.panel.AgeWorktreeReads(tt.age)

				harness.press("k", "j")
				harness.fireTimers()

				harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(tt.want)...)
			})
		}
	})

	t.Run("a directory komrad does not hold is never asked about twice", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"))
		harness.watch(buildSnapshot(buildRecordedTask()...))

		harness.press("pick:record", "pick:list", "k", "j")
		harness.fireTimers()

		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, buildWorktreeReads(1)...)
	})

	t.Run("a focused record grows to show every field and shrinks back", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{detailsHeight: 5})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecordedTask()...))
		harness.fireTimers()
		share := len(readRecord(harness.readFrame()))
		require.Negative(t, findRecordLine(harness.readFrame(), "created"), "the last fields are past the share")

		harness.press("pick:record")

		frame := harness.readFrame()
		assert.Positive(t, findRecordLine(frame, "created"))
		assert.NotContains(t, strings.Join(readRecord(frame), "\n"), "more", "nothing is left out")
		assert.Len(t, splitLines(frame), defaultHarnessHeight)
		assert.Equal(t, []string{"backend", "└─ task-a1"}, readListRows(frame), "the list keeps its rows")

		harness.press("pick:list")
		assert.Len(t, readRecord(harness.readFrame()), share)
	})

	t.Run("a focused record taller than the pane grows only as far as the room", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{height: 16})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecordedTask()...))
		harness.press("k")
		harness.fireTimers()

		harness.press("pick:record")

		frame := harness.readFrame()
		assert.Len(t, splitLines(frame), 16)
		assert.Contains(t, strings.Join(readRecord(frame), "\n"), "more")
	})
}

// TestAgentsRecordSearch covers the one search over a focused record.
func TestAgentsRecordSearch(t *testing.T) {
	t.Run("typing lights the match and moves nothing until enter walks onto it", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z")

		frame := harness.readFrame()
		assert.Regexp(t, `^directory\s`, readRecordCursor(frame))
		assert.Contains(t, frame, "/ siz", "the header's search line is the record's")

		harness.press("enter")
		assert.Regexp(t, `^size\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("n and N step between the matches once a search stands", func(t *testing.T) {
		harness := openAgentRecord(t)

		harness.press("n")
		require.Regexp(t, `^agent\s`, readRecordCursor(harness.readFrame()), "nothing stands to step through")

		harness.press("/", "_", "i", "d", "enter")
		assert.Regexp(t, `^agent_id\s`, readRecordCursor(harness.readFrame()))

		harness.press("n")
		assert.Regexp(t, `^session_id\s`, readRecordCursor(harness.readFrame()))

		harness.press("N")
		assert.Regexp(t, `^agent_id\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("tab narrows the record to the fields it matches and the rows under them", func(t *testing.T) {
		harness := openAgentRecord(t)

		harness.press(append([]string{"/", "_", "i", "d", "tab", "enter"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "▽ _id")
		assert.Equal(t, []string{
			"› agent_id   a1",
			"  session_id abc",
		}, slices.DeleteFunc(readRecord(frame), isBlank))
	})

	t.Run("a filter nothing matches says so and leaves the list alone", func(t *testing.T) {
		harness := openAgentRecord(t)

		harness.press("/", "z", "z", "z", "tab", "enter")

		frame := harness.readFrame()
		assert.Contains(t, strings.Join(readRecord(frame), "\n"), "nothing matches")
		assert.Contains(t, readLegend(frame), "back to the list", "the record keeps the keys")
		assert.Equal(t, []string{"backend", "└─ auth"}, readListRows(frame))
	})

	t.Run("escape abandons the input, then clears the search, then hands the keys back", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z", "esc")

		frame := harness.readFrame()
		assert.Equal(t, "/", readSearch(frame))
		assert.Regexp(t, `^directory\s`, readRecordCursor(frame), "the record keeps the keys and its cursor")

		harness.press("/", "s", "i", "z", "enter", "esc")

		frame = harness.readFrame()
		assert.Equal(t, "/", readSearch(frame), "the first escape clears the search")
		assert.NotEmpty(t, readRecordCursor(frame))

		harness.press("esc")
		assert.Empty(t, readRecordCursor(harness.readFrame()), "the second hands the keys back")
	})

	t.Run("a search standing when the keys go keeps narrowing the record", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z", "tab", "enter", "pick:record")

		frame := harness.readFrame()
		assert.Equal(t, "▽ siz", readSearch(frame))
		assert.Len(t, slices.DeleteFunc(readRecord(frame), isBlank), 1)
	})

	t.Run("the search goes with the screen", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z", "enter", "pick:record", "tab", "shift+tab")

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Equal(t, "/", readSearch(frame))
	})

	t.Run("a focused record keeps the keys through its search in a tight pane", func(t *testing.T) {
		tests := []struct {
			name       string
			params     panelHarnessParams
			press      []string
			wantLegend bool
		}{
			{
				name: "a pane the share only just fits",
				params: panelHarnessParams{
					width:  80,
					height: 13,
				},
			},
			{
				name: "one row taller",
				params: panelHarnessParams{
					width:  80,
					height: 14,
				},
			},
			{
				name: "a narrow pane with the legend expanded",
				params: panelHarnessParams{
					width:  38,
					height: 44,
				},
				press:      []string{"?"},
				wantLegend: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := newPanelHarness(t, tt.params)
				harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
				harness.watch(buildSnapshot(buildRecorded()...))
				harness.press(tt.press...)
				if tt.wantLegend {
					require.Contains(t, readLegend(harness.readFrame()), "hide help", "the legend is expanded")
				}
				harness.press("pick:record")
				require.NotEmpty(t, readRecordCursor(harness.readFrame()), "the share fits, so the record takes the keys")

				harness.press("/", "a")

				frame := harness.readFrame()
				assert.NotEmpty(t, readRecord(frame), "the record is still rendered with its search line")
				assert.Len(t, splitLines(frame), tt.params.height)

				harness.press("enter")

				frame = harness.readFrame()
				assert.NotEmpty(t, readRecordCursor(frame), "enter kept the search rather than reaching the list")
				assert.Equal(t, agentsScreen, harness.readScreen())

				harness.press("esc", "esc")
				assert.Empty(t, readRecordCursor(harness.readFrame()), "two escapes clear the search and hand the keys back")
			})
		}
	})

	t.Run("a slash after a shrink took the keys opens the list's search", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{
			width:  80,
			height: 30,
		})
		harness.send(tea.WindowSizeMsg{
			Width:  80,
			Height: 11,
		})

		harness.press("/", "a", "g", "e", "n", "t", "3")

		assert.Contains(t, harness.readFrame(), "/ agent3")
	})

	t.Run("a grown record with no room for a row renders nothing rather than two bare rules", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:         40,
			height:        24,
			detailsHeight: 2,
		})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("pick:record", "?", "/", "a", "tab", "enter")

		frame := harness.readFrame()
		assert.False(t, isRecordRendered(frame))
		assert.False(t, hasAdjacentRules(frame), "no bare rules stand where the record was")
		assert.Len(t, splitLines(frame), 24)
	})
}

// TestAgentsSearchAcrossSections covers one search standing over both the list and the record.
func TestAgentsSearchAcrossSections(t *testing.T) {
	t.Run("a filter typed on the list narrows the list alone", func(t *testing.T) {
		harness := openAgents(t, append(buildRecorded(), newAgentAt("billing", "w1", 1))...)
		harness.press("j")
		fields := len(readRecord(harness.readFrame()))

		harness.press("/", "a", "u", "t", "h", "tab", "enter")

		frame := harness.readFrame()
		assert.Equal(t, []string{"backend", "└─ auth"}, readListRows(frame))
		assert.Len(t, readRecord(frame), fields, "the record keeps every field")
	})

	t.Run("the focus key while typing jumps into the record's first match and back out moves nothing", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("/", "a", "b", "c", "pick:record")

		frame := harness.readFrame()
		assert.Regexp(t, `^session_id\s+abc$`, readRecordCursor(frame))
		assert.Contains(t, frame, "/ abc", "the pattern stands")
		assert.NotContains(t, frame, "/ abc▏", "and the input is closed")

		harness.press("pick:list")
		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()), "the list never moved")

		harness.press("esc", "/", "b", "a", "c", "k", "enter", "pick:record")
		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()), "enter walked the list onto the header before the jump")
	})

	t.Run("clearing the search where it opened puts the cursor back and from the record leaves it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("k", "/", "a", "u", "t", "enter")
		require.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))

		harness.press("esc")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("/", "a", "u", "t", "enter", "pick:record", "esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "/ aut")
		assert.NotEmpty(t, readRecordCursor(frame), "the record keeps the keys")

		harness.press("pick:list")
		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("the page keys move the record's cursor ten fields at a time", func(t *testing.T) {
		rows := buildRecorded()
		rows[0].(*workflowv1.Workflow).Worktree.CopyPaths = []string{".env"}
		harness := openAgents(t, rows...)
		harness.press("k", "pick:record")
		first := readRecordCursor(harness.readFrame())

		harness.press("ctrl+d")
		assert.Equal(t, 10, findRecordCursor(harness.readFrame()))

		harness.press("ctrl+u")
		assert.Equal(t, first, readRecordCursor(harness.readFrame()))

		harness.press("G")
		last := readRecordCursor(harness.readFrame())
		harness.press("ctrl+d")
		assert.Equal(t, last, readRecordCursor(harness.readFrame()), "a page past the end stays on the last field")
	})

	t.Run("the match keys walk only the section holding the keys", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("k", "/", "b", "a", "c", "k", "enter", "n")

		frame := harness.readFrame()
		assert.Equal(t, "backend", readHoveredRow(frame), "n wraps to the list's only match")
		assert.Empty(t, readRecordCursor(frame))

		harness.press("pick:record", "n")
		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()), "and in the record wraps to the record's")
	})

	t.Run("enter walks the list only onto a row the list matches", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("/", "b", "a", "c", "k")
		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()), "typing moves nothing")

		harness.press("enter")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("esc", "/", "a", "b", "c", "enter")
		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()), "a pattern only the record matches leaves the list where it is")
	})

	t.Run("escape after the jump into the record clears the search and leaves both cursors", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("/", "a", "b", "c", "pick:record")

		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "/ abc")
		assert.Regexp(t, `^session_id\s+abc$`, readRecordCursor(frame))
	})

	t.Run("a filter matching only a field empties the list and the record with it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("/", "a", "b", "c", "tab", "enter")

		frame := harness.readFrame()
		assert.Empty(t, readListRows(frame))
		assert.Empty(t, readRecordCursor(frame))
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
			harness.press("j")

			frame := harness.readFrame()
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
			agent: newPausable(""),
		},
		{
			name:          "a paused agent renders that it is paused over its saved screen",
			agent:         newPausable("1970-01-01T00:00:00Z"),
			want:          []string{"paused", "saved screen", "the migration is written"},
			wantScreenFor: []string{"a1"},
		},
		{
			name: "a paused agent whose pane died is still rendered as paused",
			agent: func() *agentv1.Agent {
				agent := newPausable("1970-01-01T00:00:00Z")
				agent.Alive = xptr.Ptr(false)

				return agent
			}(),
			want:          []string{"paused", "saved screen", "the migration is written"},
			wantAbsent:    []string{"dormant · press"},
			wantScreenFor: []string{"a1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{})
			answerWorktreeReads(harness)
			answerLastScreens(harness, map[string]string{"a1": "⏺ the migration is written"})
			harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), tt.agent))
			harness.press("j")
			harness.fireTimers()

			frame := harness.readFrame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.assertSentTo(agentv1connect.AgentServiceGetLastScreenProcedure, xslice.Map(tt.wantScreenFor, func(id string) proto.Message {
				return &agentv1.GetLastScreenRequest{AgentId: id}
			})...)
		})
	}

	t.Run("an agent that runs and stops again under the cursor renders its newer screen", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		answerWorktreeReads(harness)
		answerLastScreens(harness, map[string]string{"a1": "⏺ the migration is written"})
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newDormant("login")))
		harness.press("j")
		harness.fireTimers()
		require.Contains(t, harness.readFrame(), "the migration is written")

		live := newDormant("login")
		live.Alive = xptr.Ptr(true)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), live))
		answerLastScreens(harness, map[string]string{"a1": "⏺ the tests pass"})
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newDormant("login")))

		assert.Contains(t, harness.readFrame(), "the tests pass")
	})

	t.Run("a saved screen taller than the window opens on its bottom", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		answerWorktreeReads(harness)
		answerLastScreens(harness, map[string]string{
			"a1": "❯ the first ask\n" + strings.Repeat("working through it\n", 60) + "⏺ the migration is written",
		})
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newDormant("login")))
		harness.press("j")
		harness.fireTimers()

		frame := harness.readFrame()
		assert.Contains(t, frame, "the migration is written")
		assert.NotContains(t, frame, "the first ask")
	})
}

// TestAgentsEdit covers opening your editor on an agent's worktree from its row.
func TestAgentsEdit(t *testing.T) {
	t.Run("the edit key stops the agent and opens its worktree beside the list", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))

		harness.press("space", "e")

		harness.assertSentTo(agentv1connect.AgentServiceHoldAgentProcedure, &agentv1.HoldAgentRequest{Id: "a1"})
		require.NotEmpty(t, harness.panel.ReadRespawns())
		assert.Contains(t, harness.panel.ReadRespawns()[0], "'/work/telescope'")
		assert.Equal(t, "/work/telescope", harness.panel.ReadRespawnDirs()[0])
	})

	t.Run("closing the editor lets the agent go", func(t *testing.T) {
		tests := []struct {
			name  string
			close func(*panelHarness)
		}{
			{
				name:  "the key again",
				close: func(h *panelHarness) { h.press("space", "e") },
			},
			{
				name: "the editor quitting",
				close: func(h *panelHarness) {
					editors := h.panel.ReadRespawnPanes()
					h.panel.SetPaneDead(editors[len(editors)-1])
					h.fireTimers()
				},
			},
			{
				name:  "leaving the agents list",
				close: func(h *panelHarness) { h.press("shift+tab", "shift+tab") },
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
				said := recordRelease(harness)
				harness.press("space", "e")
				require.Empty(t, *said)

				tt.close(harness)

				require.Len(t, *said, 1)
				assert.Equal(t, "a1", (*said)[0].GetId())
				assert.Contains(t, (*said)[0].GetSaid(), "while I had your worktree open")
			})
		}
	})

	t.Run("a task's edit opens the worktree its writer was provisioned, not a newer agent's own directory", func(t *testing.T) {
		writer := newEditable("")
		task := newTaskOf(writer)
		adopter := newPausable("")
		adopter.Id = "a2"
		adopter.Name = "adopter"
		adopter.PaneId = "%8"
		adopter.Cwd = "/work/repo"
		adopter.CreatedAt = "2026-09-20T13:00:00Z"
		adopter.ProvisionDisabled = true
		adopter.TaskId = writer.TaskId
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), writer, adopter, task)

		harness.press("space", "e")

		harness.assertSentTo(agentv1connect.AgentServiceHoldAgentProcedure, &agentv1.HoldAgentRequest{Id: "a1"})
		require.NotEmpty(t, harness.panel.ReadRespawnDirs())
		assert.Equal(t, "/work/telescope", harness.panel.ReadRespawnDirs()[0])
	})

	t.Run("a snapshot leaves alone the pause the editor holds", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
		said := recordRelease(harness)
		harness.press("space", "e")
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newEditable(heldAt)))
		require.Empty(t, *said)

		harness.press("space", "e")
		require.Len(t, *said, 1)
		assert.Contains(t, (*said)[0].GetSaid(), "while I had your worktree open")
	})

	t.Run("moving the cursor keeps the editor on the stage", func(t *testing.T) {
		other := newEditable("")
		other.Id, other.Name, other.PaneId = "a2", "comet", "%10"
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""), other)
		moveToAgentRow(t, harness, "telescope")
		harness.press("space", "e")
		respawns, swaps := harness.panel.CountRespawns(), harness.panel.CountSwaps()

		harness.press("j")
		harness.fireTimers()

		assert.Equal(t, respawns, harness.panel.CountRespawns())
		assert.Equal(t, swaps, harness.panel.CountSwaps())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
	})

	t.Run("an agent paused by somebody else opens no editor", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(heldAt))

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
		assert.Contains(t, harness.readFrame(), "could not edit")
	})

	t.Run("a dormant agent is neither stopped nor let go", func(t *testing.T) {
		agent := newEditable("")
		agent.Alive = xptr.Ptr(false)
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

		harness.press("space", "e")
		require.NotEmpty(t, harness.panel.ReadRespawns())
		harness.press("space", "e")

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
	})

	t.Run("an agent that will not stop opens no editor", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
		harness.answer(agentv1connect.AgentServiceHoldAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, errors.New("the agent did not stop")
		})

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
	})

	t.Run("an unreadable pane refuses the key and stops nothing", func(t *testing.T) {
		agent := newEditable("")
		agent.State = agentv1.AgentState_AGENT_STATE_UNCERTAIN
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

		harness.press("space", "e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("an edit given up before the agent stops lets it go", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
		said := recordRelease(harness)
		harness.hold(agentv1connect.AgentServiceHoldAgentProcedure)
		harness.press("space", "e")

		harness.press("esc", "shift+tab", "shift+tab")
		harness.release(agentv1connect.AgentServiceHoldAgentProcedure)

		assert.Empty(t, harness.panel.ReadRespawns())
		require.Len(t, *said, 1)
		assert.Contains(t, (*said)[0].GetSaid(), "without opening it")
	})
}

// TestAgentsStrandedPause covers a pause no screen of the panel holds.
func TestAgentsStrandedPause(t *testing.T) {
	t.Run("the snapshot lets it go and tells it why", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		answerWorktreeReads(harness)
		said := recordRelease(harness)

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newPausable(heldAt)))

		require.Len(t, *said, 1)
		assert.Equal(t, "a1", (*said)[0].GetId())
		assert.Contains(t, (*said)[0].GetSaid(), "lost track of the pause")
	})

	t.Run("an agent nobody paused is left alone", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(""))

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
	})

	t.Run("a pause another agent holds", func(t *testing.T) {
		holder := newPausable("")
		holder.Id, holder.Name, holder.PaneId = "a2", "conductor", "%10"

		tests := []struct {
			name     string
			rows     []proto.Message
			wantSent int
		}{
			{
				name: "is left alone while that agent is listed",
				rows: []proto.Message{newLiveWorkflow("w1", "backend"), holder},
			},
			{
				name:     "is let go once that agent is gone",
				rows:     []proto.Message{newLiveWorkflow("w1", "backend")},
				wantSent: 1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				held := newPausable(heldAt)
				held.HeldByAgentId = "a2"
				harness := newPanelHarness(t, panelHarnessParams{})
				answerWorktreeReads(harness)
				said := recordRelease(harness)

				harness.watch(buildSnapshot(append(tt.rows, held)...))

				assert.Len(t, *said, tt.wantSent)
			})
		}
	})

	t.Run("a paused agent renders its document in place of its pane", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(heldAt))

		assert.True(t, harness.panel.IsZoomed(), "the panel keeps the window")
		assert.Contains(t, harness.readFrame(), "Paused.")
	})

	tests := []struct {
		name  string
		state agentv1.AgentState
		want  string
	}{
		{
			name:  "at rest, which is what a pause claims",
			state: agentv1.AgentState_AGENT_STATE_IDLE,
			want:  "paused: you stopped it and have its worktree",
		},
		{
			name:  "writing again",
			state: agentv1.AgentState_AGENT_STATE_THINKING,
			want:  "paused, but it has started working again",
		},
		{
			name:  "stopped at a prompt anybody could answer",
			state: agentv1.AgentState_AGENT_STATE_WAITING,
			want:  "paused, but stopped at a prompt, so a write lands if anybody answers",
		},
		{
			name:  "a pane nobody can read",
			state: agentv1.AgentState_AGENT_STATE_UNCERTAIN,
			want:  "paused, but its pane cannot be read, so it may be writing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newPausable(heldAt)
			agent.State = tt.state
			harness := newPanelHarness(t, panelHarnessParams{width: 240})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), agent))

			harness.press(slices.Repeat([]string{"}"}, 30)...)

			record := strings.Join(readRecord(harness.readFrame()), "\n")
			assert.Contains(t, record, tt.want)
			assert.Equal(t, 1, strings.Count(record, "paused"), "a row says one thing about a pause, not two")
		})
	}
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
				harness.fireTimers()
			}

			assert.Equal(t, tt.wantZoomed, harness.panel.IsZoomed())
			if tt.want != "" {
				assert.Contains(t, harness.readFrame(), tt.want)
			}
		})
	}

	t.Run("a running orchestrator whose frame reads as no harness is staged at once", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: &harnessFrameFake{}})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newOrchestrator()))

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("an orchestrator setting up is staged once it is running", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: &harnessFrameFake{}})
		answerWorktreeReads(harness)
		settingUp := newOrchestrator()
		settingUp.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), settingUp))
		harness.fireTimers()
		require.Empty(t, harness.panel.ReadGuestPane())

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newOrchestrator()))

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("open on a workflow starts its orchestrator", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))

		harness.press("enter")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceStartOrchestratorProcedure,
			msg:       &agentv1.StartOrchestratorRequest{WorkflowId: "w1"},
		})
	})

	t.Run("moving onto a workflow starts nothing", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newAgent("a1", "telescope", "w1"))

		harness.press("k")

		require.Equal(t, "backend", readHoveredRow(harness.readFrame()))
		harness.assertSentTo(agentv1connect.AgentServiceStartOrchestratorProcedure)
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

		harness.press("f", "shift+tab", "j")

		assert.Equal(t, []string{"telescope", "  └ reviewer"}, readListRows(harness.readFrame()))
	})

	t.Run("a workflow made in the panel comes with its orchestrator", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))
		answerWorkflowCreate(harness)

		harness.press("space", "w", "a", "p", "i", "enter")

		harness.assertSentWith(ignoringEventPrompts,
			request{
				procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
				msg: &workflowv1.CreateWorkflowRequest{
					Name:     "api",
					Worktree: &workflowv1.WorktreeConfig{Enabled: true},
				},
			},
			request{
				procedure: agentv1connect.AgentServiceStartOrchestratorProcedure,
				msg:       &agentv1.StartOrchestratorRequest{WorkflowId: "w2"},
			},
		)
	})

	t.Run("a workflow stands when its orchestrator will not start", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))
		answerWorkflowCreate(harness)
		harness.refuse(agentv1connect.AgentServiceStartOrchestratorProcedure, connect.NewError(connect.CodePermissionDenied, errors.New("the person refused the directory")))

		harness.press("space", "w", "a", "p", "i", "enter")
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newLiveWorkflow("w2", "api")))

		frame := harness.readFrame()
		assert.NotContains(t, frame, "Could not create the workflow")
		assert.Equal(t, "api", readHoveredRow(frame), "the cursor lands on the workflow it made")
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
			want:  `Permanently delete task "task-fix-gutter"?`,
		},
		{
			name:  "a task whose writer has reviewers",
			press: []string{"j", "space", "a", "d"},
			want:  `Permanently delete task "task-writer"?`,
		},
		{
			name:  "a selection names its agents and the reviewers going with them",
			press: []string{"j", "v", "j", "v", "space", "a", "d"},
			want:  "Permanently delete 2 agents and 1 reviewer?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildReviewedTasks()...)

			harness.press(tt.press...)

			assert.Contains(t, harness.readFrame(), tt.want)
		})
	}

	t.Run("the hard delete counts the reviewers in the archive's own rows", func(t *testing.T) {
		writer := newArchivedAgent("a1", "writer", "w1")
		writerTask := newTaskOf(writer)
		reviewer := newArchivedAgent("a3", "reviewer", "w1")
		reviewer.TaskId = writer.TaskId
		reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
		harness := openArchivedAgents(t, newLiveWorkflow("w1", "komrad"), writer, writerTask, reviewer)

		harness.press("j", "v", "j", "v", "space", "a", "d")

		frame := harness.readFrame()
		assert.Contains(t, frame, `Permanently delete agent "writer" and its reviewer?`)
		assert.Contains(t, frame, "Komrad deletes their transcripts too")
	})

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

		harness.press("j", "space", "g")

		assert.NotRegexp(t, readOffEntry("rebase with base", "r"), harness.readRawFrame())
	})
}

// TestAgentsWatchedEdits covers edits the watch delivers reaching the hovered row's record and document.
func TestAgentsWatchedEdits(t *testing.T) {
	t.Run("a workflow context edited elsewhere is rendered without the cursor moving", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "api")
		workflow.Context = "old notes"
		harness := openAgents(t, workflow)
		require.Contains(t, harness.readFrame(), "old notes")

		harness.watch(buildPuts(&workflowv1.Workflow{
			Id:      "w1",
			Name:    "api",
			Context: "new notes",
		}))

		frame := harness.readFrame()
		assert.Contains(t, frame, "new notes")
		assert.NotContains(t, frame, "old notes")
	})

	t.Run("a pull request for a branch nobody is on waits for the task that arrives on it", func(t *testing.T) {
		agent, task := newBranchAgent("a1", "api", "agent/x")
		harness := openAgentsWith(t, panelHarnessParams{detailsHeight: 14},
			newLiveWorkflow("w1", "api"),
			agent,
			task,
			&pullrequestv1.Repository{Url: repositoryURL},
		)
		harness.press(widenListPresses...)

		harness.watch(buildPuts(&pullrequestv1.PullRequest{
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
		require.Negative(t, findRecordLine(harness.readFrame(), "pull requests"), "the row on another branch is untouched")

		arrived, arrivedTask := newBranchAgent("a2", "web", "agent/other")
		arrived.CreatedAt = "2026-09-20T12:00:00Z"
		harness.watch(buildPuts(arrived, arrivedTask))
		harness.press("k")

		frame := harness.readFrame()
		require.Equal(t, "├─ task-a2", readHoveredRow(frame))
		assert.Equal(t, "#5 closed", readRecordField(frame, "pull requests"))
	})

	t.Run("a pull request's title and author reach the record of the task on its branch", func(t *testing.T) {
		agent, task := newBranchAgent("a1", "api", "agent/x")
		harness := openAgentsWith(t, panelHarnessParams{detailsHeight: 14},
			newLiveWorkflow("w1", "api"),
			agent,
			task,
			&pullrequestv1.Repository{Url: repositoryURL},
		)
		harness.press(widenListPresses...)

		harness.watch(buildPuts(&pullrequestv1.PullRequest{
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

		frame := harness.readFrame()
		assert.Equal(t, "#7 Pull more pull request info", readRecordField(frame, "pull request"))
		assert.Equal(t, "@octocat", readRecordField(frame, "author"))
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
			harness.press(tt.press...)

			harness.press("space", "a")
			harness.assertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure)
			harness.press("enter")

			harness.assertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, readArchiveTaskRequests(tt.wantTasks)...)
		})
	}

	t.Run("a folder with no rows under it opens no selection", func(t *testing.T) {
		harness := openAgents(t, append(buildSelectable(), newLiveWorkflow("w3", "chores"))...)

		harness.press("j", "j", "j")
		require.Equal(t, "chores", readHoveredRow(harness.readFrame()))

		harness.press("v")
		assert.NotContains(t, readLegend(harness.readFrame()), "selected")
	})

	t.Run("keys with nothing to say about a set stand down and escape clears it", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("v")

		harness.press("r")
		assert.NotContains(t, harness.readFrame(), "enter submit", "rename does not open its form")

		harness.press("j")
		assert.Equal(t, "├─ api", readHoveredRow(harness.readFrame()), "the cursor still moves")

		harness.press("esc")
		assert.NotContains(t, readLegend(harness.readFrame()), "selected")
	})

	t.Run("an agent gone from the listing drops out of the selection", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)
		harness.press("v", "G", "v")

		ui := newAgentAt("ui", "w2", 1)
		harness.watch(buildSnapshot(newLiveWorkflow("w2", "frontend"), ui, newTaskOf(ui)))
		harness.press("space", "a", "enter")

		harness.assertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, readArchiveTaskRequests([]string{"task-ui"})...)
	})

	t.Run("so does one the hidden toggle puts away", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)
		harness.press("space", "h", "H", "v", "j", "j", "j", "v", "space", "h", "H")

		harness.press("space", "a", "enter")

		harness.assertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, readArchiveTaskRequests([]string{"task-auth"})...)
	})
}

func readArchiveTaskRequests(ids []string) []proto.Message {
	return xslice.Map(ids, func(id string) proto.Message { return &taskv1.ArchiveTaskRequest{Task: id} })
}

const (
	hiddenMark    = " \uf4c5"
	heldAt        = "2026-09-20T12:00:00Z"
	repositoryURL = "https://github.com/harrisoncramer/komrad"
)

var (
	recordAgentLine  = regexp.MustCompile(`agent\s+—`)
	recordBranchLine = regexp.MustCompile(`branch\s+agent/auth`)
	recordChangeLine = regexp.MustCompile(`changes\s+2 modified`)
)

func openAgents(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	return openAgentsWith(t, panelHarnessParams{}, rows...)
}

func openAgentsWith(t *testing.T, params panelHarnessParams, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(rows...))

	return harness
}

func answerWorktreeReads(harness *panelHarness) {
	harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
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
	for _, line := range splitLines(frame) {
		if pattern.MatchString(ansi.Strip(line)) {
			return line
		}
	}

	return ""
}

func openCopyRecord(t *testing.T) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(buildRecorded()...))

	return harness
}

func openAgentRecord(t *testing.T) *panelHarness {
	t.Helper()

	harness := openAgents(t, buildRecorded()...)
	harness.press("pick:record")

	return harness
}

func openWorktreeRecord(t *testing.T, params panelHarnessParams, recorded ...proto.Message) *panelHarness {
	t.Helper()

	if len(recorded) == 0 {
		recorded = buildRecordedTask()
	}
	harness := newPanelHarness(t, params)
	harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
	harness.watch(buildSnapshot(recorded...))
	harness.press("pick:record")

	return harness
}

func hasAdjacentRules(frame string) bool {
	lines := splitLines(frame)
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i-1], "─") && strings.HasPrefix(lines[i], "─") {
			return true
		}
	}

	return false
}

func readSearch(frame string) string {
	for _, line := range readBelowViews(frame) {
		prompt := strings.TrimSpace(ansi.Truncate(line, ui.SidebarCols, ""))
		if strings.HasPrefix(prompt, "/") || strings.HasPrefix(prompt, "▽") {
			return prompt
		}
	}

	return ""
}

func findRecordCursor(frame string) int {
	return slices.IndexFunc(readRecord(frame), func(line string) bool {
		return strings.HasPrefix(line, "› ")
	})
}

func countCursors(frame string) int {
	return strings.Count(strings.Join(readBelowViews(frame), "\n"), "›")
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
	for _, line := range splitLines(frame) {
		if strings.HasPrefix(line, "─") {
			tag := strings.TrimPrefix(strings.Trim(strings.TrimRight(line, " │┤┼"), "─ "), "Info")

			return strings.Trim(tag, "─ ")
		}
	}

	return ""
}

func answerWith(read *worktreev1.GetWorktreeResponse) answerFunc {
	return func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(read), nil
	}
}

func refuseWith(code connect.Code, reason string) answerFunc {
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

func newPausable(held string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "a1",
		Name:       "telescope",
		WorkflowId: "w1",
		PaneId:     "%9",
		Alive:      xptr.Ptr(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
		HeldAt:     held,
	}
}

func recordRelease(harness *panelHarness) *[]*agentv1.ReleaseAgentRequest {
	var said []*agentv1.ReleaseAgentRequest
	harness.answer(agentv1connect.AgentServiceReleaseAgentProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		release, _ := req.Any().(*agentv1.ReleaseAgentRequest)
		said = append(said, release)

		return connect.NewResponse(&agentv1.ReleaseAgentResponse{}), nil
	})

	return &said
}

func answerWorkflowCreate(harness *panelHarness) {
	harness.answer(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
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

func newEditable(held string) *agentv1.Agent {
	agent := newPausable(held)
	agent.Cwd = "/work/telescope"

	return agent
}

// TestAgentsRowShowsTheTool covers the tool an agent reported, drawn after its name while it works.
func TestAgentsRowShowsTheTool(t *testing.T) {
	agent := newPausable("")
	agent.State = agentv1.AgentState_AGENT_STATE_THINKING
	agent.ReportedReason = "WebFetch"
	harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

	row := findRowWith(t, harness.readFrame(), "telescope")
	assert.Contains(t, row, "WebFetch")
}
