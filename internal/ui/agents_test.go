package ui_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui"
	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
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
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	hiddenMark    = " \uf4c5"
	heldAt        = "2026-09-20T12:00:00Z"
	repositoryURL = "https://github.com/harrisoncramer/komrad"
)

var (
	recordNameLine   = regexp.MustCompile(`name\s+auth`)
	recordChangeLine = regexp.MustCompile(`changes\s+2 modified`)
	recordRemoteLine = regexp.MustCompile(`remote\s+↑2 unpushed`)
	cursorRowLine    = regexp.MustCompile(`package greet`)
)

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
				newLiveWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newAgent("a2", "orphan", "vanished"),
			},
			want: []string{"backend", "  auth"},
		},
		{
			name: "an archived agent and workflow are left to the archive",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newArchivedWorkflow("w2", "old"),
				newAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "done", "w1"),
			},
			want: []string{"backend", "  auth"},
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
			want: []string{"backend", "  auth", "frontend", "  ui"},
		},
		{
			name: "a child is drawn under its parent rather than where the listing put it",
			rows: buildFamily(),
			want: []string{"komrad", "  fix-gutter", "  author", "  └ test-runner", "  └ reviewer"},
		},
		{
			name: "a child whose parent is not listed stands on its own",
			rows: []proto.Message{
				newLiveWorkflow("w1", "komrad"),
				newAgentAt("fix-gutter", "w1", 2),
				newChild("a3", "reviewer", "author"),
				newChild("a4", "test-runner", "author"),
			},
			want: []string{"komrad", "  fix-gutter", "  test-runner", "  reviewer"},
		},
		{
			name: "a hidden agent gives its row to its workflow's count",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newHiddenAgent("a2", "chore"),
				newHiddenAgent("a3", "sweep"),
			},
			want: []string{"backend +2", "  auth"},
		},
		{
			name: "a hidden parent takes its children with it",
			rows: hideAgent(buildFamily(), "author"),
			want: []string{"komrad +3", "  fix-gutter"},
		},
		{
			name: "a hidden child counts on its parent rather than on the workflow",
			rows: hideAgent(buildFamily(), "a3"),
			want: []string{"komrad", "  fix-gutter", "  author +1", "  └ test-runner"},
		},
		{
			name: "an orchestrator is drawn on its workflow's row rather than as a row of its own",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newOrchestrator(),
				newAgent("a1", "telescope", "w1"),
			},
			want: []string{"backend", "  telescope"},
		},
		{
			name: "an orchestrator's child stands on its own",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newOrchestrator(),
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

	t.Run("a row opens at the gutter and every kind of row stands its bar in one column", func(t *testing.T) {
		harness := openAgents(t,
			&workflowv1.Workflow{
				Id:       "w1",
				Name:     "komrad",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
			newAgentAt("worker", "w1", 1),
			newAgentAt("other", "w1", 0),
		)

		rows := slices.DeleteFunc(splitLines(harness.readFrame()), func(line string) bool {
			_, isRow := cutListRow(line)

			return !isRow
		})
		require.Len(t, rows, 3)
		other := rows[2]
		assert.Len(t, other[:len(other)-len(strings.TrimLeft(other, " "))], 2, "an agent opens on its checkout column after the focus cell")
		for _, row := range rows[1:] {
			assert.Equal(t, findBarColumn(rows[0]), findBarColumn(row), "the bar stands in one column whatever the row is")
		}
	})

	t.Run("a branch draws nothing on the row", func(t *testing.T) {
		branch := newAgent("a1", "worker", "w1")
		branch.Branch = xptr.Ptr("agent/a1")

		plain := openAgents(t, newLiveWorkflow("w1", "backend"), newAgent("a1", "worker", "w1"))
		branched := openAgents(t, newLiveWorkflow("w1", "backend"), branch)

		assert.Equal(t, readListRows(plain.readFrame()), readListRows(branched.readFrame()))
	})

	t.Run("an empty panel welcomes a newcomer and says where to start", func(t *testing.T) {
		harness := openAgents(t)

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, frame, "Welcome, komrad!")
		assert.Contains(t, frame, "Press 'w' to set up your first workflow")
		assert.Empty(t, readListRows(frame))
		ui.RequireGolden(t, "agents-welcome", harness.readRawFrame())
	})
}

// TestAgentsShowHidden covers the key that shows and puts away the hidden agents.
func TestAgentsShowHidden(t *testing.T) {
	rows := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		newAgent("a1", "auth", "w1"),
		newHiddenAgent("a2", "chore"),
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

		assert.Equal(t, []string{"komrad", "  fix-gutter", "  author" + hiddenMark, "  └ test-runner", "  └ reviewer"}, readListRows(harness.readFrame()))
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
			want: []string{"gadgets", "  echo", "komrad", "  delta", "  Zulu", "  alpha", "  └ charlie", "  └ bob"},
		},
		{
			name:  "created up is the oldest family first",
			press: []string{"f", "k"},
			want:  []string{"gadgets", "  echo", "komrad", "  alpha", "  └ bob", "  └ charlie", "  Zulu", "  delta"},
		},
		{
			name:  "activity down is the family whose head changed state last first",
			press: []string{"f", "tab", "j"},
			want:  []string{"gadgets", "  echo", "komrad", "  Zulu", "  alpha", "  └ bob", "  └ charlie", "  delta"},
		},
		{
			name:  "name up sorts a name in capitals as its lowercase",
			press: []string{"f", "tab", "tab", "k"},
			want:  []string{"gadgets", "  echo", "komrad", "  alpha", "  └ bob", "  └ charlie", "  delta", "  Zulu"},
		},
		{
			name:  "name down",
			press: []string{"f", "tab", "tab", "j"},
			want:  []string{"gadgets", "  echo", "komrad", "  Zulu", "  delta", "  alpha", "  └ charlie", "  └ bob"},
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
			harness := newPanelHarness(t, panelHarnessParams{height: 21})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(rows...))

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Len(t, splitLines(frame), 21)
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
			want: "  ui",
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
			want: "  ui",
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

		harness.press("pick:record", "j")

		frame := harness.readFrame()
		assert.Regexp(t, `^agent\s+—$`, readRecordCursor(frame))
		assert.Contains(t, readLegend(frame), "esc back to the list")

		harness.press("pick:list")

		frame = harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "  auth", readHoveredRow(frame), "the list kept its row")

		harness.press("k")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()), "the list has the keys again")
	})

	t.Run("one cursor is drawn at a time", func(t *testing.T) {
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

		harness.press("z", "r", "n", "c")

		assert.Equal(t, before, harness.readFrame())
		harness.assertSent(buildWorktreeReadRequests(2)...)
	})

	t.Run("nothing listed gives the record no letter", func(t *testing.T) {
		harness := openAgents(t)

		harness.press("ctrl+space", "j")

		assert.Empty(t, harness.panel.ReadPickLetter("record"))
	})

	t.Run("a record shortened under the cursor holds it on its last field", func(t *testing.T) {
		rows := buildRecorded()
		agent, _ := rows[1].(*agentv1.Agent)
		agent.Branch = xptr.Ptr("agent/auth")
		harness := openAgents(t, rows...)
		harness.press("pick:record", "G")
		require.Regexp(t, `^created\s`, readRecordCursor(harness.readFrame()))

		agent.Branch = nil
		harness.watch(buildSnapshot(rows...))

		assert.Regexp(t, `^created\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("the row going from under a focused record draws the next from its top", func(t *testing.T) {
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
			name: "the whole value is taken rather than the clipped line it is drawn on",
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
			press: []string{"pick:record", "j", "j", "j", "j", "j"},
			want:  strings.Repeat("--append-system-prompt read every file on the branch ", 4),
		},
		{
			name: "a coloured value is copied as its plain text",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openWorktreeRecord(t, panelHarnessParams{})
			},
			press: []string{"/", "u", "n", "p", "u", "s", "h", "e", "d", "enter"},
			want:  "↑2 unpushed ↓1 unpulled",
		},
		{
			name: "a narrowed record copies from the rows that are left",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openWorktreeRecord(t, panelHarnessParams{})
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
		harness := openAgents(t, buildRecorded()...)

		harness.press("/", "a", "u", "t", "h")

		assert.Contains(t, findRawLine(harness.readRawFrame(), recordNameLine), paint.Panel.Match().Render("auth"))
	})

	t.Run("a search standing over the record lights the matched run of a value", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("/", "m", "o", "d", "i", "f", "i", "e", "d", "enter")

		assert.Contains(t, findRawLine(harness.readRawFrame(), recordChangeLine), paint.Panel.Match().Render("modified"))
	})

	t.Run("the tag and the counts wear the tone of what they count", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})
		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.readRawFrame()
		for _, want := range []string{
			paint.Panel.Style(paint.ToneSuccess).Render("+200"),
			paint.Panel.Style(paint.ToneDanger).Render("−100"),
			paint.Panel.Style(paint.ToneWarning).Bold(true).Render("↑2 unpushed"),
			paint.Panel.Style(paint.ToneMark).Render("↓1 unpulled"),
		} {
			assert.Contains(t, frame, want)
		}
	})

	t.Run("a count of nothing is dimmed", func(t *testing.T) {
		read := buildWorktreeRead()
		read.Unpulled = 0
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(read))
		harness.watch(buildSnapshot(buildRecorded()...))
		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		assert.Contains(t, harness.readRawFrame(), errui.Dim.Render("↓0 unpulled"))
	})

	t.Run("a remote whose unpulled commits landed on the base dims what landed", func(t *testing.T) {
		tests := []struct {
			name  string
			ahead int32
			want  string
		}{
			{
				name:  "a branch carrying work of its own keeps the warning on what it has not pushed",
				ahead: 3,
				want:  paint.Panel.Style(paint.ToneWarning).Bold(true).Render("↑2 unpushed") + " " + errui.Dim.Render("↓1 unpulled · landed on main"),
			},
			{
				name:  "a branch carrying nothing of its own is dimmed whole",
				ahead: 0,
				want:  errui.Dim.Render("↑2 unpushed") + " " + errui.Dim.Render("↓1 unpulled · landed on main"),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				read := buildWorktreeRead()
				read.UnpulledLanded, read.Ahead = true, tt.ahead
				harness := newPanelHarness(t, panelHarnessParams{})
				harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(read))
				harness.watch(buildSnapshot(buildRecorded()...))
				harness.press(append([]string{"pick:record"}, widenListPresses...)...)

				assert.Contains(t, harness.readRawFrame(), tt.want)
			})
		}
	})

	t.Run("a search over a coloured value lights its match in the plain text", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})
		harness.press(append([]string{"/", "u", "n", "p", "u", "s", "h", "e", "d", "enter"}, widenListPresses...)...)

		line := findRawLine(harness.readRawFrame(), recordRemoteLine)
		assert.Contains(t, line, paint.Panel.Match().Render("unpushed"))
		assert.NotContains(t, line, paint.Panel.Style(paint.ToneWarning).Bold(true).Render("↑2 unpushed"))
	})

	t.Run("the hovered label takes the accent only while the record has the keys", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		label := "name      "
		require.Contains(t, findRawLine(harness.readRawFrame(), recordNameLine), errui.Dim.Render(label))

		harness.press("pick:record")

		lit := findRawLine(harness.readRawFrame(), recordNameLine)
		assert.Contains(t, lit, paint.Panel.Cursor().Render(label))
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
			assert.Equal(t, tt.want > 0, isRecordDrawn(frame))
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
		rows := buildRecorded()
		agent, _ := rows[1].(*agentv1.Agent)
		agent.Branch = xptr.Ptr("agent/auth")
		harness := openAgents(t, rows...)
		require.Equal(t, "  ↓ 3 more", readRecord(harness.readFrame())[8])

		harness.press("pick:record", "G", "pick:list")

		record := readRecord(harness.readFrame())
		assert.Equal(t, "  ↑ 3 more", record[0])
		assert.NotContains(t, record, "  ↓ 3 more")
	})
}

// TestAgentsRecordFocusAndSize covers the record giving up the keys when the pane has no room to draw it.
func TestAgentsRecordFocusAndSize(t *testing.T) {
	rows := append(buildRecorded(), newAgentAt("billing", "w1", 1))

	tests := []struct {
		name   string
		params panelHarnessParams
	}{
		{
			name:   "a pane too short to draw the record takes no letter",
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
		assert.Equal(t, "  auth", readHoveredRow(frame), "the list answered the key pressed while it was small")
		assert.Empty(t, readRecordCursor(frame))

		harness.press("k")
		assert.Equal(t, "  billing", readHoveredRow(harness.readFrame()), "the keys are still with the list")

		harness.press("pick:record")
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
			name:       "a directory komrad does not hold draws no worktree rows and no error",
			answer:     refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"),
			wantAbsent: []string{"worktree", "size", "remote"},
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
			wantAbsent: []string{"remote", "commits"},
		},
		{
			name:   "a readable worktree says where its branch stands against its remote and leaves the base to the worktree screen",
			answer: answerWith(buildWorktreeRead()),
			want: map[string]string{
				"remote":    "↑2 unpushed ↓1 unpulled",
				"changes":   "2 modified, 1 untracked",
				"landed":    "1 commit the base already holds",
				"sequence":  "a merge or rebase is part-way through",
				"also here": "reader",
			},
			wantAbsent: []string{"uncommitted", "base"},
		},
		{
			name: "a branch with no commits of its own has nothing to push",
			answer: answerWith(func() *worktreev1.GetWorktreeResponse {
				read := buildWorktreeRead()
				read.Pushed, read.Ahead = false, 0

				return read
			}()),
			want: map[string]string{"remote": "nothing to push"},
		},
		{
			name: "a branch never pushed says so",
			answer: answerWith(func() *worktreev1.GetWorktreeResponse {
				read := buildWorktreeRead()
				read.Pushed = false

				return read
			}()),
			want: map[string]string{"remote": "not pushed"},
		},
		{
			name: "a branch level with its remote reads even",
			answer: answerWith(func() *worktreev1.GetWorktreeResponse {
				read := buildWorktreeRead()
				read.Unpushed, read.Unpulled = 0, 0

				return read
			}()),
			want: map[string]string{"remote": "even"},
		},
		{
			name: "a remote whose unpulled commits landed on the base says so",
			answer: answerWith(func() *worktreev1.GetWorktreeResponse {
				read := buildWorktreeRead()
				read.UnpulledLanded = true

				return read
			}()),
			want: map[string]string{"remote": "↑2 unpushed ↓1 unpulled · landed on main"},
		},
		{
			name: "a detached head has no remote row",
			answer: answerWith(func() *worktreev1.GetWorktreeResponse {
				read := buildWorktreeRead()
				read.Worktree.Branch = ""

				return read
			}()),
			wantAbsent: []string{"remote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{})
			harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, tt.answer)
			harness.watch(buildSnapshot(buildRecorded()...))

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

	t.Run("the standing follows what was launched", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.readFrame()
		directory, remote, size, flags := findRecordLine(frame, "directory"), findRecordLine(frame, "remote"), findRecordLine(frame, "size"), findRecordLine(frame, "flags")
		require.Positive(t, directory)
		assert.Less(t, directory, remote)
		assert.Less(t, size, flags, "the standing comes before the ids")
		assert.Negative(t, findRecordLine(frame, "commits"))
	})

	t.Run("the top rule carries the lines the branch changed", func(t *testing.T) {
		tests := []struct {
			name   string
			answer answerFunc
			want   string
		}{
			{
				name:   "a directory komrad does not hold carries nothing",
				answer: refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"),
			},
			{
				name: "a branch with no base to count against carries nothing",
				answer: answerWith(func() *worktreev1.GetWorktreeResponse {
					read := buildWorktreeRead()
					read.BaseRef, read.BaseError = "", "no base branch"
					read.AddedLines, read.RemovedLines = nil, nil

					return read
				}()),
			},
			{
				name: "a base the branch shares no history with carries nothing",
				answer: answerWith(func() *worktreev1.GetWorktreeResponse {
					read := buildWorktreeRead()
					read.AddedLines, read.RemovedLines = nil, nil

					return read
				}()),
			},
			{
				name: "a branch with nothing changed reads clean",
				answer: answerWith(func() *worktreev1.GetWorktreeResponse {
					read := buildWorktreeRead()
					read.AddedLines, read.RemovedLines = xptr.Ptr[int32](0), xptr.Ptr[int32](0)

					return read
				}()),
				want: "clean",
			},
			{
				name:   "a branch with changes counts the lines added and removed",
				answer: answerWith(buildWorktreeRead()),
				want:   "+200 −100",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := newPanelHarness(t, panelHarnessParams{})
				harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, tt.answer)
				harness.watch(buildSnapshot(buildRecorded()...))

				assert.Equal(t, tt.want, readRecordTag(harness.readFrame()))
			})
		}
	})

	t.Run("the focus key reads the directory again", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.assertSent(buildWorktreeReadRequests(1)...)

		harness.press("pick:record")

		harness.assertSent(buildWorktreeReadRequests(2)...)
	})

	t.Run("a refused reread keeps the last answer beside the refusal", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))
		harness.refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("context deadline exceeded")))

		harness.press(append([]string{"pick:record"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Equal(t, "could not read: context deadline exceeded", readRecordField(frame, "worktree"))
		assert.Equal(t, "↑2 unpushed ↓1 unpulled", readRecordField(frame, "remote"))
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
				harness := openAgents(t, buildRecorded()...)
				harness.panel.AgeWorktreeReads(tt.age)

				harness.press("k", "j")
				harness.fireTimers()

				harness.assertSent(buildWorktreeReadRequests(tt.want)...)
			})
		}
	})

	t.Run("a directory komrad does not hold is never asked about twice", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, refuseWith(connect.CodeNotFound, "/repo is not under the worktree directories"))
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("pick:record", "pick:list", "k", "j")

		harness.assertSent(buildWorktreeReadRequests(1)...)
	})

	t.Run("a focused record grows to show every field and shrinks back", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))
		share := len(readRecord(harness.readFrame()))
		require.Negative(t, findRecordLine(harness.readFrame(), "created"), "the last fields are past the share")

		harness.press("pick:record")

		frame := harness.readFrame()
		assert.Positive(t, findRecordLine(frame, "created"))
		assert.NotContains(t, strings.Join(readRecord(frame), "\n"), "more", "nothing is left out")
		assert.Len(t, splitLines(frame), defaultHarnessHeight)
		assert.Equal(t, []string{"backend", "  auth"}, readListRows(frame), "the list keeps its rows")

		harness.press("pick:list")
		assert.Len(t, readRecord(harness.readFrame()), share)
	})

	t.Run("a focused record taller than the pane grows only as far as the room", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{height: 16})
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
		harness.watch(buildSnapshot(buildRecorded()...))

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
		assert.Regexp(t, `^name\s`, readRecordCursor(frame))
		assert.Contains(t, frame, "/ siz", "the header's search line is the record's")

		harness.press("enter")
		assert.Regexp(t, `^size\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("n and N step between the matches once a search stands", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press("n")
		require.Regexp(t, `^name\s`, readRecordCursor(harness.readFrame()), "nothing stands to step through")

		harness.press("/", "_", "i", "d", "enter")
		assert.Regexp(t, `^agent_id\s`, readRecordCursor(harness.readFrame()))

		harness.press("n")
		assert.Regexp(t, `^session_id\s`, readRecordCursor(harness.readFrame()))

		harness.press("N")
		assert.Regexp(t, `^agent_id\s`, readRecordCursor(harness.readFrame()))
	})

	t.Run("tab narrows the record to the fields it matches and the rows under them", func(t *testing.T) {
		harness := openWorktreeRecord(t, panelHarnessParams{})

		harness.press(append([]string{"/", "_", "i", "d", "tab", "enter"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "▽ _id")
		assert.Equal(t, []string{
			"› agent_id   a1",
			"  session_id abc",
		}, slices.DeleteFunc(readRecord(frame), isBlank))
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

		harness.press("/", "a", "u", "t", "tab", "enter", "pick:record")

		frame := harness.readFrame()
		assert.Equal(t, "▽ aut", readSearch(frame))
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
					width:  30,
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
				assert.NotEmpty(t, readRecord(frame), "the record is still drawn with its search line")
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

	t.Run("a grown record with no room for a row draws nothing rather than two bare rules", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:         40,
			height:        18,
			detailsHeight: 2,
		})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildRecorded()...))

		harness.press("pick:record", "?", "/", "a", "tab", "enter")

		frame := harness.readFrame()
		assert.False(t, isRecordDrawn(frame))
		assert.False(t, hasAdjacentRules(frame), "no bare rules stand where the record was")
		assert.Len(t, splitLines(frame), 18)
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

		harness.press("/", "a", "b", "c", "pick:record")

		frame := harness.readFrame()
		assert.Regexp(t, `^session_id\s+abc$`, readRecordCursor(frame))
		assert.Contains(t, frame, "/ abc", "the pattern stands")
		assert.NotContains(t, frame, "/ abc▏", "and the input is closed")

		harness.press("pick:list")
		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()), "the list never moved")

		harness.press("esc", "/", "b", "a", "c", "k", "enter", "pick:record")
		assert.Regexp(t, `^name\s+backend$`, readRecordCursor(harness.readFrame()), "enter walked the list onto the header before the jump")
	})

	t.Run("clearing the search where it opened puts the cursor back and from the record leaves it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("k", "/", "a", "u", "t", "enter")
		require.Equal(t, "  auth", readHoveredRow(harness.readFrame()))

		harness.press("esc")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("/", "a", "u", "t", "enter", "pick:record", "esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "/ aut")
		assert.NotEmpty(t, readRecordCursor(frame), "the record keeps the keys")

		harness.press("pick:list")
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

		harness.press("pick:record", "n")
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

// TestAgentsEdit covers opening your editor on an agent's worktree from its row.
func TestAgentsEdit(t *testing.T) {
	t.Run("the edit key stops the agent and opens its worktree beside the list", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))

		harness.press("e")

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
				close: func(h *panelHarness) { h.press("e") },
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
				harness.press("e")
				require.Empty(t, *said)

				tt.close(harness)

				require.Len(t, *said, 1)
				assert.Equal(t, "a1", (*said)[0].GetId())
				assert.Contains(t, (*said)[0].GetSaid(), "while I had your worktree open")
			})
		}
	})

	t.Run("the pause keys leave alone the agent being edited", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
		said := recordRelease(harness)
		harness.press("e")
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newEditable(heldAt)))

		harness.press("p", "P")
		require.Empty(t, *said)

		harness.press("e")
		require.Len(t, *said, 1)
		assert.Contains(t, (*said)[0].GetSaid(), "while I had your worktree open")
	})

	t.Run("moving the cursor keeps the editor on the stage", func(t *testing.T) {
		other := newEditable("")
		other.Id, other.Name, other.PaneId = "a2", "comet", "%10"
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""), other)
		moveToAgentRow(t, harness, "telescope")
		harness.press("e")
		respawns, swaps := harness.panel.CountRespawns(), harness.panel.CountSwaps()

		harness.press("j")
		harness.fireTimers()

		assert.Equal(t, respawns, harness.panel.CountRespawns())
		assert.Equal(t, swaps, harness.panel.CountSwaps())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
	})

	t.Run("an agent that is not the editor's to stop is not let go", func(t *testing.T) {
		tests := []struct {
			name  string
			agent *agentv1.Agent
		}{
			{
				name:  "one you paused yourself",
				agent: newEditable(heldAt),
			},
			{
				name: "a dormant one",
				agent: func() *agentv1.Agent {
					agent := newEditable("")
					agent.Alive = xptr.Ptr(false)

					return agent
				}(),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, newLiveWorkflow("w1", "backend"), tt.agent)

				harness.press("e")
				require.NotEmpty(t, harness.panel.ReadRespawns())
				harness.press("e")

				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceReleaseAgentProcedure))
			})
		}
	})

	t.Run("an agent that will not stop opens no editor", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
		harness.answer(agentv1connect.AgentServiceHoldAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, errors.New("the agent did not stop")
		})

		harness.press("e")

		assert.Empty(t, harness.panel.ReadRespawns())
	})

	t.Run("an unreadable pane refuses the key and stops nothing", func(t *testing.T) {
		agent := newEditable("")
		agent.State = agentv1.AgentState_AGENT_STATE_UNCERTAIN
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), agent)

		harness.press("e")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceHoldAgentProcedure))
	})

	t.Run("an edit given up before the agent stops lets it go", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newEditable(""))
		said := recordRelease(harness)
		harness.hold(agentv1connect.AgentServiceHoldAgentProcedure)
		harness.press("e")

		harness.press("shift+tab", "shift+tab")
		harness.release(agentv1connect.AgentServiceHoldAgentProcedure)

		assert.Empty(t, harness.panel.ReadRespawns())
		require.Len(t, *said, 1)
		assert.Contains(t, (*said)[0].GetSaid(), "without opening it")
	})
}

// TestAgentsPause covers pausing an agent and letting it go again.
func TestAgentsPause(t *testing.T) {
	t.Run("pausing takes the agent and says nothing to it", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(""))

		harness.press("p")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceHoldAgentProcedure,
			msg:       &agentv1.HoldAgentRequest{Id: "a1"},
		})
	})

	t.Run("unpausing tells the agent to carry on", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(heldAt))
		said := recordRelease(harness)

		harness.press("p")

		require.Len(t, *said, 1)
		assert.Equal(t, "a1", (*said)[0].GetId())
		assert.NotEmpty(t, (*said)[0].GetSaid(), "an unpaused agent is told to carry on rather than left stopped")
	})

	t.Run("unpausing in silence asks first", func(t *testing.T) {
		tests := []struct {
			name     string
			answer   string
			wantSent int
		}{
			{
				name:   "declining keeps the hold",
				answer: "n",
			},
			{
				name:     "confirming lets it go without saying anything",
				answer:   "y",
				wantSent: 1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(heldAt))
				said := recordRelease(harness)

				harness.press("P")
				require.Empty(t, *said, "the key asks before it drops the words")

				harness.press(tt.answer)

				require.Len(t, *said, tt.wantSent)
				if tt.wantSent > 0 {
					assert.Equal(t, "a1", (*said)[0].GetId())
					assert.Empty(t, (*said)[0].GetSaid(), "nothing is typed at an agent let go in silence")
				}
			})
		}
	})

	t.Run("unpausing in silence is only for a paused agent", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(""))
		before := harness.readFrame()

		harness.press("P")

		assert.Equal(t, before, harness.readFrame(), "no prompt opens")
		harness.assertSent()
	})

	t.Run("the open key does nothing for a paused agent", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(heldAt))

		harness.press("enter")

		harness.assertSent()
		assert.Contains(t, harness.readFrame(), "Press unpause to let it carry on")
	})

	t.Run("a paused agent draws its document in place of its pane", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(heldAt))

		assert.True(t, harness.panel.IsZoomed(), "the panel keeps the window")
		assert.Contains(t, harness.readFrame(), "Press unpause to let it carry on")
	})

	t.Run("the legend names the way out of a pause only on a paused row", func(t *testing.T) {
		paused := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(heldAt))
		paused.press(append(widenListPresses, "?")...)
		assert.Contains(t, readLegend(paused.readFrame()), "unpause in silence")

		loose := openAgents(t, newLiveWorkflow("w1", "backend"), newPausable(""))
		loose.press(append(widenListPresses, "?")...)
		assert.NotContains(t, readLegend(loose.readFrame()), "unpause in silence")
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
			want:       "Press open to resume.",
			wantZoomed: true,
		},
		{
			name:       "a workflow that never had one draws its shared context",
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

	t.Run("a running orchestrator whose harness draws late is staged once it draws", func(t *testing.T) {
		frames := &drawReporter{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newOrchestrator()))
		harness.fireTimers()
		require.Empty(t, harness.panel.ReadGuestPane())

		frames.isDrawn = true
		harness.fireTimers()

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
		harness.assertSent()
	})

	t.Run("a flat list draws no row for the orchestrator it folded away", func(t *testing.T) {
		harness := openAgents(t,
			newLiveWorkflow("w1", "backend"),
			newOrchestrator(),
			newAgentAt("telescope", "w1", 1),
			newChild("a2", "reader", "telescope"),
		)

		harness.press("f", "shift+tab", "j")

		assert.Equal(t, []string{"telescope", "  └ reader"}, readListRows(harness.readFrame()))
	})

	t.Run("a workflow made in the panel comes with its orchestrator", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"))
		answerWorkflowCreate(harness, "w2")

		harness.press("w", "a", "p", "i", "ctrl+s")

		harness.assertSent(
			request{
				procedure: hookv1connect.HookServiceListHooksProcedure,
				msg:       &emptypb.Empty{},
			},
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
		answerWorkflowCreate(harness, "w2")
		harness.refuse(agentv1connect.AgentServiceStartOrchestratorProcedure, connect.NewError(connect.CodePermissionDenied, errors.New("the person refused the directory")))

		harness.press("w", "a", "p", "i", "ctrl+s")
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newLiveWorkflow("w2", "api")))

		frame := harness.readFrame()
		assert.NotContains(t, frame, "Could not create the workflow")
		assert.Equal(t, "api", readHoveredRow(frame), "the cursor lands on the workflow it made")
	})
}

// TestAgentsChildren covers what a parent and its children allow and say about each other.
func TestAgentsChildren(t *testing.T) {
	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "one agent with nothing attached",
			press: []string{"z"},
			want:  `Archive agent "fix-gutter"?`,
		},
		{
			name:  "one agent with children",
			press: []string{"j", "z"},
			want:  `Archive agent "author" and its 2 children?`,
		},
		{
			name:  "a selection of agents counts every child that goes with it",
			press: []string{"v", "j", "v", "z"},
			want:  "Archive 2 agents and 2 children?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildFamily()...)

			harness.press(tt.press...)

			assert.Contains(t, harness.readFrame(), tt.want)
		})
	}

	t.Run("the hard delete counts the children in the archive's own rows", func(t *testing.T) {
		reviewer := newArchivedAgent("a3", "reviewer", "w1")
		reviewer.ParentAgentId = xptr.Ptr("a1")
		harness := openGroupedArchive(t, newLiveWorkflow("w1", "komrad"), newArchivedAgent("a1", "author", "w1"), reviewer)

		harness.press("j", "d")

		frame := harness.readFrame()
		assert.Contains(t, frame, `Permanently delete agent "author" and its child?`)
		assert.Contains(t, frame, "Their transcripts go with them")
	})

	t.Run("the child key on a child says why rather than opening a form", func(t *testing.T) {
		harness := openAgents(t, buildFamily()...)

		harness.press(append([]string{"G", "C"}, widenListPresses...)...)

		assert.NotContains(t, harness.readFrame(), "C-s submit")
		assert.Contains(t, readOpenMessages(harness), "may not have children")
	})

	t.Run("the review subpage on a child says why rather than opening", func(t *testing.T) {
		harness := openAgents(t, buildFamily()...)

		harness.press("G", "space", "r")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "its work is reviewed there")
	})

	t.Run("only the parent's own worktree is offered a rebase", func(t *testing.T) {
		rows := buildFamily()
		rows[0] = &workflowv1.Workflow{
			Id:       "w1",
			Name:     "komrad",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		}
		author, _ := rows[1].(*agentv1.Agent)
		author.Cwd = "/tmp/tree"
		harness := openAgents(t, rows...)

		off := readOffEntry("rebase with base", "r")

		harness.press("j", "space", "g")
		assert.NotRegexp(t, off, harness.readRawFrame())

		harness.press("esc", "esc", "j", "space", "g")
		assert.Regexp(t, off, harness.readRawFrame())
	})
}

// TestAgentsWatchedEdits covers edits the watch delivers reaching the hovered row's record and document.
func TestAgentsWatchedEdits(t *testing.T) {
	t.Run("a workflow context edited elsewhere is drawn without the cursor moving", func(t *testing.T) {
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

	t.Run("a pull request for a branch nobody is on waits for the agent that arrives on it", func(t *testing.T) {
		harness := openAgents(t,
			newLiveWorkflow("w1", "api"),
			newBranchAgent("a1", "api", "agent/x"),
			&pullrequestv1.Repository{
				Url:  repositoryURL,
				Read: true,
			},
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
		}))
		require.Equal(t, "none from this branch", readRecordField(harness.readFrame(), "pull request"), "the row on another branch is untouched")

		arrived := newBranchAgent("a2", "web", "agent/other")
		arrived.CreatedAt = "2026-09-20T12:00:00Z"
		harness.watch(buildPuts(arrived))
		harness.press("k")

		frame := harness.readFrame()
		require.Equal(t, "  web", readHoveredRow(frame))
		assert.True(t, strings.HasPrefix(readRecordField(frame, "pull request"), "#7 open"))
	})
}

// TestAgentsSelection covers choosing several agents and acting on them at once.
func TestAgentsSelection(t *testing.T) {
	tests := []struct {
		name    string
		press   []string
		wantIDs []string
	}{
		{
			name:    "with nothing selected the archive key takes the hovered row",
			wantIDs: []string{"auth"},
		},
		{
			name:    "the select key takes in the agent under the cursor",
			press:   []string{"j", "v"},
			wantIDs: []string{"api"},
		},
		{
			name:    "and drops one already selected",
			press:   []string{"v", "j", "v", "v"},
			wantIDs: []string{"auth"},
		},
		{
			name:    "a header takes its whole folder less the hidden agent with no row",
			press:   []string{"k", "v"},
			wantIDs: []string{"auth", "api", "db"},
		},
		{
			name:    "a partly selected folder is filled in rather than emptied",
			press:   []string{"j", "v", "k", "k", "v"},
			wantIDs: []string{"auth", "api", "db"},
		},
		{
			name:    "a wholly selected folder drops and leaves what is outside it",
			press:   []string{"k", "v", "G", "v", "g", "g", "v", "G"},
			wantIDs: []string{"ui"},
		},
		{
			name:    "the range key from a list at rest takes the agent under the cursor",
			press:   []string{"j", "j", "V"},
			wantIDs: []string{"db"},
		},
		{
			name:    "and on a header takes the folder",
			press:   []string{"k", "V"},
			wantIDs: []string{"auth", "api", "db"},
		},
		{
			name:    "a header takes no range once the mode is up",
			press:   []string{"j", "j", "v", "g", "g", "V", "j", "j", "j"},
			wantIDs: []string{"db"},
		},
		{
			name:    "a selection above reaches down to the cursor",
			press:   []string{"v", "j", "j", "V"},
			wantIDs: []string{"auth", "api", "db"},
		},
		{
			name:    "a selection below reaches up to it",
			press:   []string{"j", "j", "v", "k", "k", "V"},
			wantIDs: []string{"auth", "api", "db"},
		},
		{
			name:    "a header inside the range is passed over",
			press:   []string{"j", "j", "v", "G", "V"},
			wantIDs: []string{"db", "ui"},
		},
		{
			name:    "a cursor as far from a selection either side takes the one above",
			press:   []string{"v", "j", "j", "v", "k", "V"},
			wantIDs: []string{"auth", "api", "db"},
		},
		{
			name:    "a selection spanning folders goes in one call",
			press:   []string{"v", "G", "v", "g", "g", "j"},
			wantIDs: []string{"auth", "ui"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildSelectable()...)
			harness.press(tt.press...)

			harness.press("z")
			harness.assertSent()
			harness.press("ctrl+s")

			harness.assertSent(request{
				procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
				msg:       &agentv1.ArchiveAgentsRequest{Ids: tt.wantIDs},
			})
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
		assert.NotContains(t, harness.readFrame(), "C-s submit", "rename does not open its form")

		harness.press("j")
		assert.Equal(t, "  api", readHoveredRow(harness.readFrame()), "the cursor still moves")

		harness.press("esc")
		assert.NotContains(t, readLegend(harness.readFrame()), "selected")
	})

	t.Run("an agent gone from the listing drops out of the selection", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("v", "G", "v")

		harness.watch(buildSnapshot(newLiveWorkflow("w2", "frontend"), newAgentAt("ui", "w2", 1)))
		harness.press("z", "ctrl+s")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
			msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"ui"}},
		})
	})

	t.Run("so does one the hidden toggle puts away", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("H", "v", "j", "j", "j", "v", "H")

		harness.press("z", "ctrl+s")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
			msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"auth"}},
		})
	})
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

func buildFamily() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		newAgentAt("author", "w1", 1),
		newAgentAt("fix-gutter", "w1", 2),
		newChild("a3", "reviewer", "author"),
		newChild("a4", "test-runner", "author"),
	}
}

func buildSortable() []proto.Message {
	newSortable := func(name, workflowID string, created, changed int) *agentv1.Agent {
		agent := newAgentAt(name, workflowID, created)
		agent.StateChangedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", changed)

		return agent
	}
	charlie := newSortable("charlie", "w1", 25, 5)
	charlie.ParentAgentId = xptr.Ptr("alpha")
	bob := newSortable("bob", "w1", 5, 40)
	bob.ParentAgentId = xptr.Ptr("alpha")
	zulu := newSortable("zulu", "w1", 20, 30)
	zulu.Name = "Zulu"

	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		newLiveWorkflow("w2", "gadgets"),
		newSortable("delta", "w1", 30, 10),
		charlie,
		zulu,
		newSortable("alpha", "w1", 10, 20),
		bob,
		newSortable("echo", "w2", 0, 50),
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

func newHiddenAgent(id, name string) *agentv1.Agent {
	agent := newAgent(id, name, "w1")
	agent.Hidden = true

	return agent
}

func newChild(id, name, parentID string) *agentv1.Agent {
	agent := newAgent(id, name, "w1")
	agent.ParentAgentId = xptr.Ptr(parentID)

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

func openWorktreeRecord(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(buildWorktreeRead()))
	harness.watch(buildSnapshot(buildRecorded()...))
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

func findBarColumn(row string) int {
	for _, bar := range listBars {
		if at := strings.Index(row, strings.TrimSpace(bar)); at >= 0 {
			return ansi.StringWidth(row[:at])
		}
	}

	return -1
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
		AddedLines:         xptr.Ptr[int32](200),
		RemovedLines:       xptr.Ptr[int32](100),
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
			return strings.Trim(strings.TrimRight(line, " │┤┼"), "─ ")
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

func buildWorktreeReadRequests(count int) []request {
	return slices.Repeat([]request{{
		procedure: worktreev1connect.WorktreeServiceGetWorktreeProcedure,
		msg:       &worktreev1.GetWorktreeRequest{Dir: "/repo"},
	}}, count)
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

func answerWorkflowCreate(harness *panelHarness, id string) {
	harness.answer(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: id}}), nil
	})
}

func newBranchAgent(id, name, branch string) *agentv1.Agent {
	agent := newAgent(id, name, "w1")
	agent.RepositoryUrl = xptr.Ptr(repositoryURL)
	agent.Branch = xptr.Ptr(branch)

	return agent
}

func buildSelectable() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		newAgentAt("auth", "w1", 5),
		newAgentAt("api", "w1", 4),
		newAgentAt("db", "w1", 3),
		newHiddenAgent("sweep", "sweep"),
		newAgentAt("ui", "w2", 1),
	}
}

func newEditable(held string) *agentv1.Agent {
	agent := newPausable(held)
	agent.Cwd = "/work/telescope"

	return agent
}
