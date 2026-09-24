package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const hiddenMark = " \uf4c5"

// TestAgentsRows covers which rows the agents tree draws and where.
func TestAgentsRows(t *testing.T) {
	tests := []struct {
		name string
		rows []proto.Message
		want []string
	}{
		{
			name: "an agent whose workflow is gone is not drawn",
			rows: []proto.Message{
				newWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newAgent("a2", "orphan", "vanished"),
			},
			want: []string{"backend", "  auth"},
		},
		{
			name: "an archived agent and workflow are left to the archive",
			rows: []proto.Message{
				newWorkflow("w1", "backend"),
				newArchivedWorkflow("w2", "old"),
				newAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "done", "w1"),
			},
			want: []string{"backend", "  auth"},
		},
		{
			name: "an empty workflow keeps its header",
			rows: []proto.Message{newWorkflow("w1", "backend")},
			want: []string{"backend"},
		},
		{
			name: "agents nest under the workflow they belong to",
			rows: []proto.Message{
				newWorkflow("w1", "backend"),
				newWorkflow("w2", "frontend"),
				newAgent("a1", "auth", "w1"),
				newAgent("a2", "ui", "w2"),
			},
			want: []string{"backend", "  auth", "frontend", "  ui"},
		},
		{
			name: "a child is drawn under its parent rather than where the listing put it",
			rows: buildFamily(),
			want: []string{"weave", "  fix-gutter", "  author", "  └ test-runner", "  └ reviewer"},
		},
		{
			name: "a child whose parent is not listed stands on its own",
			rows: []proto.Message{
				newWorkflow("w1", "weave"),
				newAgentAt("fix-gutter", "w1", 2),
				newChild("a3", "reviewer", "author"),
				newChild("a4", "test-runner", "author"),
			},
			want: []string{"weave", "  fix-gutter", "  test-runner", "  reviewer"},
		},
		{
			name: "a hidden agent gives its row to its workflow's count",
			rows: []proto.Message{
				newWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newHiddenAgent("a2", "chore", "w1"),
				newHiddenAgent("a3", "sweep", "w1"),
			},
			want: []string{"backend +2", "  auth"},
		},
		{
			name: "a hidden parent takes its children with it",
			rows: hideAgent(buildFamily(), "author"),
			want: []string{"weave +3", "  fix-gutter"},
		},
		{
			name: "a hidden child counts on its parent rather than on the workflow",
			rows: hideAgent(buildFamily(), "a3"),
			want: []string{"weave", "  fix-gutter", "  author +1", "  └ test-runner"},
		},
		{
			name: "an orchestrator is drawn on its workflow's row rather than as a row of its own",
			rows: []proto.Message{
				newWorkflow("w1", "backend"),
				newOrchestrator("o1", "w1"),
				newAgent("a1", "telescope", "w1"),
			},
			want: []string{"backend", "  telescope"},
		},
		{
			name: "an orchestrator's child stands on its own",
			rows: []proto.Message{
				newWorkflow("w1", "backend"),
				newOrchestrator("o1", "w1"),
				newChild("a1", "reader", "o1"),
			},
			want: []string{"backend", "  reader"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, tt.rows...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("an empty panel still draws its chrome and says where to start", func(t *testing.T) {
		harness := openAgents(t)

		frame := harness.readFrame()
		assert.Contains(t, frame, agentsTitle)
		assert.Contains(t, frame, "nothing here yet")
		assert.Empty(t, readListRows(frame))
	})
}

// TestAgentsShowHidden covers the key that shows and puts away the hidden agents.
func TestAgentsShowHidden(t *testing.T) {
	rows := []proto.Message{
		newWorkflow("w1", "backend"),
		newWorkflow("w2", "frontend"),
		newAgent("a1", "auth", "w1"),
		newHiddenAgent("a2", "chore", "w1"),
		newAgent("a3", "ui", "w2"),
	}

	t.Run("a hidden agent comes back under its own workflow and the cursor keeps its row", func(t *testing.T) {
		harness := openAgents(t, rows...)
		harness.press("j", "j")
		require.Equal(t, "  ui", readHoveredRow(harness.readFrame()))

		harness.press("H")

		frame := harness.readFrame()
		assert.Equal(t, []string{"backend", "  chore" + hiddenMark, "  auth", "frontend", "  ui"}, readListRows(frame))
		assert.Equal(t, "  ui", readHoveredRow(frame))
	})

	t.Run("the hidden parent's family comes back whole", func(t *testing.T) {
		harness := openAgents(t, hideAgent(buildFamily(), "author")...)

		harness.press("H")

		assert.Equal(t, []string{"weave", "  fix-gutter", "  author" + hiddenMark, "  └ test-runner", "  └ reviewer"}, readListRows(harness.readFrame()))
	})

	tests := []struct {
		name       string
		press      []string
		want       string
		wantAbsent string
	}{
		{
			name:       "on a workflow header the legend offers only the toggle",
			press:      []string{"k"},
			want:       "show hidden",
			wantAbsent: "hide/show hidden",
		},
		{
			name:       "on a listed agent the two keys share a line",
			press:      nil,
			want:       "hide/show hidden",
			wantAbsent: "unhide",
		},
		{
			name:  "on a revealed agent the line offers to unhide it",
			press: []string{"H", "k"},
			want:  "unhide/put hidden away",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, rows...)

			harness.press(append(append(tt.press, widenListPresses...), "?")...)

			legend := readLegend(harness.readFrame())
			assert.Contains(t, legend, tt.want)
			if tt.wantAbsent != "" {
				assert.NotContains(t, legend, tt.wantAbsent)
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
			name: "created down is the fallback, the newest family first",
			want: []string{"gadgets", "  echo", "weave", "  delta", "  Zulu", "  alpha", "  └ charlie", "  └ bob"},
		},
		{
			name:  "created up is the oldest family first",
			press: []string{"f", "k"},
			want:  []string{"gadgets", "  echo", "weave", "  alpha", "  └ bob", "  └ charlie", "  Zulu", "  delta"},
		},
		{
			name:  "activity down is the family whose head changed state last first",
			press: []string{"f", "tab", "j"},
			want:  []string{"gadgets", "  echo", "weave", "  Zulu", "  alpha", "  └ bob", "  └ charlie", "  delta"},
		},
		{
			name:  "name up sorts a name in capitals as its lowercase",
			press: []string{"f", "tab", "tab", "k"},
			want:  []string{"gadgets", "  echo", "weave", "  alpha", "  └ bob", "  └ charlie", "  delta", "  Zulu"},
		},
		{
			name:  "name down",
			press: []string{"f", "tab", "tab", "j"},
			want:  []string{"gadgets", "  echo", "weave", "  Zulu", "  delta", "  alpha", "  └ charlie", "  └ bob"},
		},
		{
			name:  "a flat list drops the headers and walks every workflow's families together",
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
		require.Equal(t, "  alpha", readHoveredRow(harness.readFrame()))

		harness.press("f", "tab", "esc")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "walking to a tab reorders nothing")

		harness.press("f", "tab", "j", "esc")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "activity down")

		harness.press("f", "tab", "k", "esc")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "name up")
	})

	t.Run("the cursor keeps its agent across the flip to a flat list and back", func(t *testing.T) {
		harness := openAgents(t, buildSortable()...)
		require.Equal(t, "  echo", readHoveredRow(harness.readFrame()))

		harness.press("f", "shift+tab", "j", "esc")
		assert.Equal(t, "echo", readHoveredRow(harness.readFrame()), "the flat list")

		harness.press("f", "shift+tab", "k", "esc")
		assert.Equal(t, "  echo", readHoveredRow(harness.readFrame()), "the tree again")
	})
}

// TestAgentsLongList covers a list with more rows than its pane.
func TestAgentsLongList(t *testing.T) {
	rows := []proto.Message{newWorkflow("w1", "weave")}
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
			want:       "  agent-00",
			wantMore:   []string{"↓"},
			wantAbsent: []string{"↑"},
		},
		{
			name:     "in the middle both ends are",
			press:    slices.Repeat([]string{"j"}, 20),
			want:     "  agent-20",
			wantMore: []string{"↑", "↓"},
		},
		{
			name:       "at the bottom only what is above is",
			press:      []string{"G"},
			want:       "  agent-39",
			wantMore:   []string{"↑"},
			wantAbsent: []string{"↓"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{height: 20})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(rows...))

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Len(t, splitLines(frame), 20)
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

func openAgents(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
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

func buildFamily() []proto.Message {
	return []proto.Message{
		newWorkflow("w1", "weave"),
		newAgentAt("author", "w1", 1),
		newAgentAt("fix-gutter", "w1", 2),
		newChild("a3", "reviewer", "author"),
		newChild("a4", "test-runner", "author"),
	}
}

func buildSortable() []proto.Message {
	sortable := func(name, workflowID string, created, changed int) *agentv1.Agent {
		agent := newAgentAt(name, workflowID, created)
		agent.StateChangedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", changed)

		return agent
	}
	charlie := sortable("charlie", "w1", 25, 5)
	charlie.ParentAgentId = proto.String("alpha")
	bob := sortable("bob", "w1", 5, 40)
	bob.ParentAgentId = proto.String("alpha")
	zulu := sortable("zulu", "w1", 20, 30)
	zulu.Name = "Zulu"

	return []proto.Message{
		newWorkflow("w1", "weave"),
		newWorkflow("w2", "gadgets"),
		sortable("delta", "w1", 30, 10),
		charlie,
		zulu,
		sortable("alpha", "w1", 10, 20),
		bob,
		sortable("echo", "w2", 0, 50),
	}
}

func hideAgent(rows []proto.Message, id string) []proto.Message {
	for _, row := range rows {
		if agent, isAgent := row.(*agentv1.Agent); isAgent && agent.GetId() == id {
			agent.Hidden = true
		}
	}

	return rows
}

func newWorkflow(id, name string) *workflowv1.Workflow {
	return newLiveWorkflow(id, name)
}

func newAgent(id, name, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       name,
		WorkflowId: workflowID,
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
	}
}

func newAgentAt(name, workflowID string, minute int) *agentv1.Agent {
	agent := newAgent(name, name, workflowID)
	agent.CreatedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", minute)

	return agent
}

func newHiddenAgent(id, name, workflowID string) *agentv1.Agent {
	agent := newAgent(id, name, workflowID)
	agent.Hidden = true

	return agent
}

func newChild(id, name, parentID string) *agentv1.Agent {
	agent := newAgent(id, name, "w1")
	agent.ParentAgentId = proto.String(parentID)

	return agent
}

func newOrchestrator(id, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       "orchestrator",
		WorkflowId: workflowID,
		PaneId:     "%9",
		Harness:    "claude",
		Alive:      proto.Bool(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
		Role:       agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
	}
}

// TestAgentsCursor covers where the cursor stands as keys move it and the watch reshapes the tree.
func TestAgentsCursor(t *testing.T) {
	rows := []proto.Message{
		newWorkflow("w1", "backend"),
		newWorkflow("w2", "frontend"),
		newAgentAt("auth", "w1", 3),
		newAgentAt("api", "w1", 2),
		newAgentAt("db", "w1", 1),
		newAgentAt("ui", "w2", 0),
	}

	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "one press of the top key waits for the second",
			press: []string{"j", "g"},
			want:  "  api",
		},
		{
			name:  "a key between the two drops the first press",
			press: []string{"j", "j", "g", "k", "g"},
			want:  "  api",
		},
		{
			name:  "the top key twice lands on the first row",
			press: []string{"j", "g", "g"},
			want:  "backend",
		},
		{
			name:  "the bottom key lands on the last row",
			press: []string{"G"},
			want:  "  ui",
		},
		{
			name: "the first listing lands on the first agent rather than the folder above it",
			want: "  auth",
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

	reshapes := []struct {
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
				newWorkflow("w1", "backend"),
				newAgentAt("api", "w1", 3),
				newAgentAt("auth", "w1", 2),
			},
			want: "  api",
		},
		{
			name:  "an agent created elsewhere lands where the listing put it",
			press: []string{"j"},
			next: append(slices.Clone(rows),
				newAgentAt("new", "w1", 9),
			),
			want: "  api",
		},
		{
			name:  "a folder archived elsewhere leaves the cursor on the folder it named",
			press: []string{"G", "k"},
			next: []proto.Message{
				newWorkflow("w2", "frontend"),
				newAgentAt("ui", "w2", 0),
			},
			want: "frontend",
		},
		{
			name:  "a folder sorting in above keeps the cursor on its agent",
			press: []string{"G"},
			next: append(slices.Clone(rows),
				newWorkflow("w0", "api"),
				newAgentAt("spike", "w0", 4),
			),
			want: "  ui",
		},
		{
			name:  "the last agent under a folder going lands on the next agent rather than its header",
			press: []string{"j", "j"},
			next: []proto.Message{
				newWorkflow("w1", "backend"),
				newWorkflow("w2", "frontend"),
				newAgentAt("auth", "w1", 3),
				newAgentAt("api", "w1", 2),
				newAgentAt("ui", "w2", 0),
			},
			want: "  ui",
		},
	}

	for _, tt := range reshapes {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, rows...)
			harness.press(tt.press...)

			harness.watch(buildSnapshot(tt.next...))

			frame := harness.readFrame()
			assert.Contains(t, frame, agentsTitle)
			assert.Equal(t, tt.want, readHoveredRow(frame))
		})
	}
}

// TestAgentsRecord covers the record drawn under the list for the hovered row.
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
			name:  "a child leads with the parent it is attached to",
			rows:  buildFamily(),
			press: []string{"G"},
			want:  map[string]string{"name": "reviewer", "attached to": "author"},
		},
		{
			name: "an agent shows what it was launched with",
			rows: buildRecorded(),
			want: map[string]string{
				"name":       "auth",
				"workflow":   "backend",
				"directory":  "/repo",
				"agent_id":   "a1",
				"session_id": "abc",
			},
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

		harness.press("ctrl+t", "j")

		frame := harness.readFrame()
		assert.Regexp(t, `^agent\s+—$`, readRecordCursor(frame))
		assert.Contains(t, readLegend(frame), "C-t back to the list")

		harness.press("ctrl+t")

		frame = harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "  auth", readHoveredRow(frame), "the list kept its row")

		harness.press("k")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()), "the list has the keys again")
	})

	t.Run("another row's record opens at its top", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("ctrl+t", "j", "j", "ctrl+t", "k", "ctrl+t")

		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()))
	})

	t.Run("the keys that act on a row stand down while the record has them", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("ctrl+t")
		before := harness.readFrame()

		harness.press("z", "r", "n", "c")

		assert.Equal(t, before, harness.readFrame())
		harness.assertSent(readWorktreeRequests("/repo", 2)...)
	})

	t.Run("nothing listed leaves the keys with the list", func(t *testing.T) {
		harness := openAgents(t)

		harness.press("ctrl+t")

		frame := harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.NotContains(t, readLegend(frame), "back to the list")
	})

	t.Run("a record shortened under the cursor holds it on its last field", func(t *testing.T) {
		rows := buildRecorded()
		harness := openAgents(t, rows...)
		harness.press("ctrl+t", "G")
		require.Regexp(t, `^pr\s`, readRecordCursor(harness.readFrame()))

		agent, _ := rows[1].(*agentv1.Agent)
		agent.Metadata = nil
		harness.watch(buildSnapshot(rows...))

		assert.Regexp(t, `^created\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("the row going from under a focused record draws the next from its top", func(t *testing.T) {
		rows := buildRecorded()
		harness := openAgents(t, rows...)
		harness.press("ctrl+t", "j", "j", "j")

		harness.watch(buildSnapshot(rows[0]))
		require.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()))

		harness.press("j")
		assert.Regexp(t, `^agents\s+0$`, readRecordCursor(harness.readFrame()))
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
			height: 12,
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

			assert.Len(t, readRecord(harness.readFrame()), tt.want)
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
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, "  ↓ 2 more", readRecord(harness.readFrame())[8])

		harness.press("ctrl+t", "G", "ctrl+t")

		record := readRecord(harness.readFrame())
		assert.Equal(t, "  ↑ 2 more", record[0])
		assert.NotContains(t, record, "  ↓ 2 more")
	})
}

// TestAgentsRecordFocusAndSize covers the record giving up the keys when the pane has no room to draw it.
func TestAgentsRecordFocusAndSize(t *testing.T) {
	rows := append(buildRecorded(), newAgentAt("billing", "w1", 1))

	t.Run("the focus key in a pane too short to draw the record leaves the keys with the list", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{height: 12})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(rows...))

		harness.press("ctrl+t", "j")

		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("a share too small for a field leaves the keys with the list", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			height:        19,
			detailsHeight: 0.05,
		})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(rows...))

		harness.press("ctrl+t", "j")

		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("a shrink takes the keys back and a grow does not return them", func(t *testing.T) {
		harness := openAgents(t, rows...)
		harness.press("ctrl+t", "j")
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
		assert.Equal(t, "  auth", readHoveredRow(frame), "the list answered the key pressed while it was small")
		assert.Empty(t, readRecordCursor(frame))

		harness.press("k")
		assert.Equal(t, "  billing", readHoveredRow(harness.readFrame()), "the keys are still with the list")

		harness.press("ctrl+t")
		assert.NotEmpty(t, readRecordCursor(harness.readFrame()), "until the focus key is pressed again")
	})
}

// TestAgentsRecordWorktree covers the record carrying what the service read of the agent's directory.
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
			name:       "a directory weave does not hold draws no worktree rows and no error",
			answer:     refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"),
			wantAbsent: []string{"worktree", "size", "base"},
		},
		{
			name:   "a directory git refuses says why",
			answer: refuseWith(connect.CodeInternal, "git exploded"),
			want:   map[string]string{"worktree": "could not read: git exploded"},
		},
		{
			name:       "a registration git cannot read has a size and no standing",
			answer:     answerWith(unreadable),
			want:       map[string]string{"size": "1.2G, measured 0s ago"},
			wantAbsent: []string{"base", "commits"},
		},
		{
			name:   "a readable worktree says where its branch stands and what is uncommitted",
			answer: answerWith(buildWorktreeRead()),
			want: map[string]string{
				"base":        "origin/main  ↑3 ↓12",
				"changes":     "2 modified, 1 untracked",
				"landed":      "1 commit the base already holds",
				"sequence":    "a merge or rebase is part-way through",
				"also here":   "reader",
				"uncommitted": "M a.go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{})
			harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, tt.answer)
			harness.watch(buildSnapshot(buildRecorded()...))

			harness.press(append([]string{"ctrl+t"}, widenListPresses...)...)

			frame := harness.readFrame()
			for label, value := range tt.want {
				assert.Equal(t, value, readRecordField(frame, label), label)
			}
			for _, label := range tt.wantAbsent {
				assert.Negative(t, findRecordLine(frame, label), label)
			}
		})
	}

	t.Run("the standing follows what was launched and the lists come last", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press(append([]string{"ctrl+t"}, widenListPresses...)...)

		frame := harness.readFrame()
		directory, base, size, flags := findRecordLine(frame, "directory"), findRecordLine(frame, "base"), findRecordLine(frame, "size"), findRecordLine(frame, "flags")
		pr, uncommitted, commits := findRecordLine(frame, "pr"), findRecordLine(frame, "uncommitted"), findRecordLine(frame, "commits")
		require.Positive(t, directory)
		assert.Less(t, directory, base)
		assert.Less(t, size, flags, "the standing comes before the ids")
		assert.Less(t, pr, uncommitted, "the lists come after the metadata")
		assert.Equal(t, "M b.go", strings.TrimSpace(splitLines(frame)[uncommitted+1]), "a second path carries no label of its own")
		assert.Regexp(t, `^87a3bcc98c47\s+2h\s+fix\(view\): drop the column$`, readRecordField(frame, "commits"))
		assert.Positive(t, commits)
	})

	t.Run("the paths and commits past the first ten fold into a count", func(t *testing.T) {
		read := buildWorktreeRead()
		read.Modified, read.Untracked, read.Commits = nil, nil, nil
		for i := range 12 {
			read.Untracked = append(read.Untracked, fmt.Sprintf("file%d", i))
			read.Commits = append(read.Commits, &worktreev1.Commit{
				Sha:     fmt.Sprintf("%012d", i),
				Subject: "a commit",
			})
		}
		harness := newPanelHarness(t, panelHarnessParams{height: 60})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(read))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("ctrl+t")

		frame := harness.readFrame()
		assert.Equal(t, 2, strings.Count(frame, "+ 2 more"))
	})

	t.Run("the focus key reads the directory again", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.assertSent(readWorktreeRequests("/repo", 1)...)

		harness.press("ctrl+t")

		harness.assertSent(readWorktreeRequests("/repo", 2)...)
	})

	t.Run("a refused reread keeps the last answer beside the refusal", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))
		harness.refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("context deadline exceeded")))

		harness.press(append([]string{"ctrl+t"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Equal(t, "could not read: context deadline exceeded", readRecordField(frame, "worktree"))
		assert.Equal(t, "origin/main  ↑3 ↓12", readRecordField(frame, "base"))
		assert.Equal(t, "M a.go", readRecordField(frame, "uncommitted"))
	})

	t.Run("a directory weave does not hold is never asked about twice", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("ctrl+t", "ctrl+t", "k", "j")

		harness.assertSent(readWorktreeRequests("/repo", 1)...)
	})

	t.Run("a focused record grows to show every field and shrinks back", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))
		share := len(readRecord(harness.readFrame()))
		require.Negative(t, findRecordLine(harness.readFrame(), "commits"), "the last fields are past the share")

		harness.press("ctrl+t")

		frame := harness.readFrame()
		assert.Positive(t, findRecordLine(frame, "commits"))
		assert.NotContains(t, strings.Join(readRecord(frame), "\n"), "more", "nothing is left out")
		assert.Len(t, splitLines(frame), defaultHarnessHeight)
		assert.Equal(t, []string{"backend", "  auth"}, readListRows(frame), "the list keeps its rows")

		harness.press("ctrl+t")
		assert.Len(t, readRecord(harness.readFrame()), share)
	})

	t.Run("a focused record taller than the pane grows only as far as the room", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{height: 24})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("ctrl+t")

		frame := harness.readFrame()
		assert.Len(t, splitLines(frame), 24)
		assert.Contains(t, strings.Join(readRecord(frame), "\n"), "more")
	})
}

// TestAgentsRecordSearch covers the one search over a focused record.
func TestAgentsRecordSearch(t *testing.T) {
	t.Run("typing lights the match and moves nothing until enter walks onto it", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z")

		frame := harness.readFrame()
		assert.Regexp(t, `^name\s`, readRecordCursor(frame))
		assert.Contains(t, frame, "/ siz", "the header's search line is the record's")

		harness.press("enter")
		assert.Regexp(t, `^size\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("n and N step between the matches once a search stands", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("n")
		require.Regexp(t, `^name\s`, readRecordCursor(harness.readFrame()), "nothing stands to step through")

		harness.press("/", "c", "o", "m", "m", "i", "t", "enter")
		assert.Regexp(t, `^landed\s`, readRecordCursor(harness.readFrame()))

		harness.press("n")
		assert.Regexp(t, `^uncommitted\s`, readRecordCursor(harness.readFrame()))

		harness.press("N")
		assert.Regexp(t, `^landed\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("tab narrows the record to the fields it matches and the rows under them", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press(append([]string{"/", "c", "o", "m", "m", "i", "t", "tab", "enter"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "▽ commit")
		assert.Equal(t, []string{
			"› landed      1 commit the base already holds",
			"  uncommitted M a.go",
			"              M b.go",
			"              ? notes.md",
			"  commits     87a3bcc98c47  2h   fix(view): drop the column",
			"              1234567890ab  3h   feat(ui): a thing",
		}, slices.DeleteFunc(readRecord(frame), isBlank))
	})

	t.Run("a narrowed list keeps the count of what it cut", func(t *testing.T) {
		read := buildWorktreeRead()
		for i := range 12 {
			read.Untracked = append(read.Untracked, fmt.Sprintf("file%d", i))
		}
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(read))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("ctrl+t", "/", "u", "n", "c", "o", "m", "tab", "enter")

		record := slices.DeleteFunc(readRecord(harness.readFrame()), isBlank)
		require.NotEmpty(t, record)
		assert.Equal(t, "+ 5 more", strings.TrimSpace(record[len(record)-1]))
	})

	t.Run("a filter nothing matches says so and leaves the list alone", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "z", "z", "z", "tab", "enter")

		frame := harness.readFrame()
		assert.Contains(t, strings.Join(readRecord(frame), "\n"), "nothing matches")
		assert.Contains(t, readLegend(frame), "back to the list", "the record keeps the keys")
		assert.Equal(t, []string{"backend", "  auth"}, readListRows(frame))
	})

	t.Run("escape abandons the input, then clears the search, then hands the keys back", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z", "esc")

		frame := harness.readFrame()
		assert.Equal(t, "/", readSearch(frame))
		assert.Regexp(t, `^name\s`, readRecordCursor(frame), "the record keeps the keys and its cursor")

		harness.press("/", "s", "i", "z", "enter", "esc")

		frame = harness.readFrame()
		assert.Equal(t, "/", readSearch(frame), "the first escape clears the search")
		assert.NotEmpty(t, readRecordCursor(frame))

		harness.press("esc")
		assert.Empty(t, readRecordCursor(harness.readFrame()), "the second hands the keys back")
	})

	t.Run("a search standing when the keys go keeps narrowing the record", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "a", "u", "t", "tab", "enter", "ctrl+t")

		frame := harness.readFrame()
		assert.Equal(t, "▽ aut", readSearch(frame))
		assert.Len(t, slices.DeleteFunc(readRecord(frame), isBlank), 1)
	})

	t.Run("the search goes with the screen", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "s", "i", "z", "enter", "ctrl+t", "S", "esc")

		frame := harness.readFrame()
		assert.Contains(t, frame, agentsTitle)
		assert.Equal(t, "/", readSearch(frame))
	})

	t.Run("a focused record keeps the keys through its search in a tight pane", func(t *testing.T) {
		tests := []struct {
			name   string
			params panelHarnessParams
			press  []string
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
					width:  30,
					height: 26,
				},
				press: []string{"?"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := newPanelHarness(t, tt.params)
				harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
				harness.watch(buildSnapshot(buildRecorded()...))
				harness.press(tt.press...)
				harness.press("ctrl+t")
				require.NotEmpty(t, readRecordCursor(harness.readFrame()), "the share fits, so the record takes the keys")

				harness.press("/", "a")

				frame := harness.readFrame()
				assert.NotEmpty(t, readRecord(frame), "the record is still drawn with its search line")
				assert.Len(t, splitLines(frame), tt.params.height)

				harness.press("enter")

				frame = harness.readFrame()
				assert.NotEmpty(t, readRecordCursor(frame), "enter kept the search rather than reaching the list")
				assert.Contains(t, frame, agentsTitle)

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

	t.Run("a grown record with no room for a row draws nothing rather than two bare rules", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:         40,
			height:        19,
			detailsHeight: 2,
		})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("?", "ctrl+t", "/", "a", "tab", "enter")

		frame := harness.readFrame()
		assert.Empty(t, readRecord(frame))
		assert.Len(t, splitLines(frame), 19)
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
		assert.Equal(t, []string{"backend", "  auth"}, readListRows(frame))
		assert.Len(t, readRecord(frame), fields, "the record keeps every field")
	})

	t.Run("the focus key while typing jumps into the record's first match and back out moves nothing", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("/", "a", "b", "c", "ctrl+t")

		frame := harness.readFrame()
		assert.Regexp(t, `^session_id\s+abc$`, readRecordCursor(frame))
		assert.Contains(t, frame, "/ abc", "the pattern stands")
		assert.NotContains(t, frame, "/ abc▏", "and the input is closed")

		harness.press("ctrl+t")
		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()), "the list never moved")

		harness.press("esc", "/", "b", "a", "c", "k", "enter", "ctrl+t")
		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()), "enter walked the list onto the header before the jump")
	})

	t.Run("clearing the search where it opened puts the cursor back and from the record leaves it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("k", "/", "a", "u", "t", "enter")
		require.Equal(t, "  auth", readHoveredRow(harness.readFrame()))

		harness.press("esc")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("/", "a", "u", "t", "enter", "ctrl+t", "esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "/ aut")
		assert.NotEmpty(t, readRecordCursor(frame), "the record keeps the keys")

		harness.press("ctrl+t")
		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("the page keys move the record's cursor ten fields at a time", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})
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

		harness.press("ctrl+t", "n")
		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()), "and in the record wraps to the record's")
	})

	t.Run("enter walks the list only onto a row the list matches", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.press("/", "b", "a", "c", "k")
		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()), "typing moves nothing")

		harness.press("enter")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("esc", "/", "a", "b", "c", "enter")
		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()), "a pattern only the record matches leaves the list where it is")
	})

	t.Run("escape after the jump into the record clears the search and leaves both cursors", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("/", "a", "b", "c", "ctrl+t")

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

func openWorktreeRecord(t *testing.T, params panelHarnessParams, extra ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
	harness.watch(buildSnapshot(append(buildRecorded(), extra...)...))
	harness.press("ctrl+t")

	return harness
}

func readSearch(frame string) string {
	return strings.TrimSpace(ansi.Truncate(splitLines(frame)[2], config.DefaultSidebarWidth, ""))
}

func findRecordCursor(frame string) int {
	return slices.IndexFunc(readRecord(frame), func(line string) bool {
		return strings.HasPrefix(line, "› ")
	})
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
			SizeBytes:  proto.Int64(1200 * 1024 * 1024),
			MeasuredAt: proto.String(time.Now().UTC().Format(time.RFC3339)),
		},
		Modified:           []string{"a.go", "b.go"},
		Untracked:          []string{"notes.md"},
		BaseRef:            "origin/main",
		Ahead:              3,
		Behind:             12,
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

func readWorktreeRequests(dir string, count int) []request {
	return slices.Repeat([]request{{
		procedure: worktreev1connect.WorktreeServiceGetWorktreeProcedure,
		msg:       &worktreev1.GetWorktreeRequest{Dir: dir},
	}}, count)
}

func buildRecorded() []proto.Message {
	agent := newAgentAt("auth", "w1", 0)
	agent.Id = "a1"
	agent.Cwd = "/repo"
	agent.SessionId = "abc"
	agent.Metadata = []*agentv1.AgentMetadata{{
		Key:   "pr",
		Value: "https://github.com/o/r/pull/312",
	}}

	return []proto.Message{
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		agent,
	}
}
