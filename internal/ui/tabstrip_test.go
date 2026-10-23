package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestTabStrip covers the sort strip every tabbed list renders under its filter line.
func TestTabStrip(t *testing.T) {
	tests := []struct {
		name   string
		press  []string
		screen screen.ID
		second string
		kinds  string
	}{
		{
			name:   "the agents tree",
			screen: screen.Agents,
			second: "activity",
			kinds:  "F   live  archived  deleted",
		},
		{
			name:   "the archived agents",
			press:  []string{"F", "tab", "esc"},
			screen: screen.Agents,
			second: "name",
			kinds:  "F   live  archived  deleted",
		},
		{
			name:   "the worktrees",
			press:  []string{"shift+tab"},
			screen: screen.Worktrees,
			second: "last-accessed",
			kinds:  "F   all  inactive  merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})
			harness.Press(tt.press...)

			sidebar := paneltest.Sidebar(harness.Frame())
			strip := findSortStrip(t, harness)
			require.Greater(t, len(sidebar), strip+3)
			gap := strip + 1
			if tt.kinds != "" {
				assert.Equal(t, tt.kinds, sidebar[gap], "the row of kinds stands under the strip")
				gap++
			}
			assert.True(t, strings.HasPrefix(sidebar[gap], "/"), "the search stands under the strips")
			gap++
			assert.Empty(t, sidebar[gap], "one blank row stands before the list")

			harness.Press("f")
			frame := harness.Frame()
			require.Contains(t, paneltest.Legend(frame), stripLegend)
			assert.Empty(t, paneltest.HoveredRow(frame), "the row's arrow stands down")

			harness.Press("tab", "j", "k")
			assert.Contains(t, paneltest.Sidebar(harness.Frame())[strip], "↑ "+tt.second)

			harness.Press("esc")
			frame = harness.Frame()
			assert.Equal(t, tt.screen, paneltest.Screen(harness.Frame()), "the press is spent on the strip rather than on the screen's back key")
			assert.Contains(t, paneltest.Sidebar(frame)[strip], "↑ "+tt.second, "the choice is kept")
			assert.NotEmpty(t, paneltest.HoveredRow(frame), "and the list has the keys again")
		})
	}

	t.Run("enter takes the hovered stop and gives the keys back", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})
		strip := findSortStrip(t, harness)

		harness.Press("f", "tab", "enter")
		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, paneltest.Sidebar(frame)[strip], "↓ activity")
		assert.NotContains(t, paneltest.Legend(frame), stripLegend)
		assert.NotEmpty(t, paneltest.HoveredRow(frame))

		harness.Press("F", "tab", "enter")
		frame = harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.NotEmpty(t, paneltest.HoveredRow(frame))
	})

	t.Run("enter on the group stop keeps the flip j made", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})
		strip := findSortStrip(t, harness)

		harness.Press("f", "shift+tab", "j", "enter")
		frame := harness.Frame()
		assert.Contains(t, paneltest.Sidebar(frame)[strip], "flat")
		assert.NotContains(t, paneltest.Legend(frame), stripLegend)
	})

	t.Run("the record and the strip take the keys from each other", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, paneltest.BuildSelectable()...)

		harness.Press("pick:record", "f")
		frame := harness.Frame()
		assert.Contains(t, paneltest.Legend(frame), stripLegend)
		assert.Empty(t, paneltest.RecordCursor(frame))

		harness.Press("esc", "pick:record")
		assert.NotEmpty(t, paneltest.RecordCursor(harness.Frame()))
	})

	t.Run("a pane too short for the strip and a row renders no strip and takes no keys", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{
			Width:  80,
			Height: 7,
		})
		harness.Press("shift+tab", "G")
		frame := harness.Frame()
		require.NotContains(t, paneltest.Sidebar(frame)[1], "f ", "the strip gives up its row")
		assert.Contains(t, frame, "/trees/agent39", "to the row the cursor stands on")

		harness.Press("f")
		assert.NotContains(t, harness.Frame(), stripLegend)

		resize(harness, 80, 8)
		assert.NotContains(t, harness.Frame(), stripLegend, "and the refused press does not wake once the pane grows")
	})

	t.Run("a pane one row taller renders the strip and still the cursor's row", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{
			Width:  80,
			Height: 8,
		})
		harness.Press("shift+tab", "G")

		frame := harness.Frame()
		assert.Contains(t, paneltest.Sidebar(frame)[1], "f ")
		assert.Contains(t, frame, "/trees/agent39")
		assert.Len(t, paneltest.SplitLines(frame), 8)
	})

	t.Run("a shrink past the strip gives the keys back to the list for good", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{
			Width:  80,
			Height: 8,
		})
		harness.Press("shift+tab", "G", "f")
		require.Contains(t, harness.Frame(), stripLegend)

		resize(harness, 80, 7)
		assert.NotContains(t, harness.Frame(), stripLegend)

		resize(harness, 80, 8)
		harness.Press("k")
		assert.Contains(t, harness.Frame(), "/trees/agent38", "the next press moves the list")
	})
}

func findSortStrip(t *testing.T, harness *paneltest.Panel) int {
	t.Helper()

	strip := slices.IndexFunc(paneltest.Sidebar(harness.Frame()), func(line string) bool { return strings.HasPrefix(line, "f ") })
	require.GreaterOrEqual(t, strip, 0)

	return strip
}

// TestTabStripGolden renders each tabbed agents sidebar whole, grouped and flat.
func TestTabStripGolden(t *testing.T) {
	tests := []struct {
		name  string
		press []string
	}{
		{
			name:  "agents-sort-strip",
			press: []string{"j", "j"},
		},
		{
			name:  "agents-sort-strip-flat",
			press: []string{"j", "f", "tab", "j", "shift+tab", "shift+tab", "j", "esc"},
		},
		{
			name:  "archived-agents-sort-strip",
			press: []string{"F", "tab", "esc"},
		},
		{
			name:  "archived-agents-sort-strip-flat",
			press: []string{"F", "tab", "esc", "f", "shift+tab", "j", "esc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{
				Width:  44,
				Height: 28,
			})
			harness.AnswerWorktreeReads()
			harness.Watch(paneltest.Snapshot(buildPinnedArchive()...))

			harness.Press(tt.press...)

			paneltest.RequireGolden(t, tt.name, harness.RawFrame())
		})
	}
}

const stripLegend = "esc back to the list"

func buildPinnedArchive() []proto.Message {
	pinnedAt := timestamppb.New(time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local))

	live := paneltest.Agent("live", "treeview", "w1")
	live.Harness = "claude"
	live.Cwd = "/trees/live"
	live.Alive = xptr.Ptr(true)
	live.State = agentv1.AgentState_AGENT_STATE_THINKING
	live.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	live.TaskId = xptr.Ptr("live")

	occupant := paneltest.Agent("occ", "review-607", "w1")
	occupant.Cwd = "/trees/live"
	occupant.Alive = xptr.Ptr(true)

	archived := paneltest.Agent("archived", "reviewer", "w1")
	archived.Harness = "claude"
	archived.Cwd = "/trees/archived"
	archived.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	archived.ArchivedAt = pinnedAt
	archived.TaskId = xptr.Ptr("archived")

	admin := paneltest.Agent("admin", "cargo-fix", "w1")
	admin.Cwd = "/trees/admin"
	admin.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	admin.ArchivedAt = pinnedAt
	admin.TaskId = xptr.Ptr("admin")

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		live,
		occupant,
		archived,
		admin,
		&taskv1.Task{Id: "live", WorkflowId: "w1", Slug: "treeview", Cwd: xptr.Ptr("/trees/live")},
		&taskv1.Task{Id: "archived", WorkflowId: "w1", Slug: "reviewer", Cwd: xptr.Ptr("/trees/archived"), ArchivedAt: paneltest.ArchivedAt},
		&taskv1.Task{Id: "admin", WorkflowId: "w1", Slug: "cargo-fix", Cwd: xptr.Ptr("/trees/admin"), ArchivedAt: paneltest.ArchivedAt},
		&worktreev1.WatchedWorktree{Dir: "/trees/admin", RepoDir: "/home/h/gadgets/admin", Branch: "task/admin", Readable: true},
		&worktreev1.WatchedWorktree{Dir: "/trees/archived", RepoDir: "/home/h/komrad", Branch: "task/archived", Readable: true},
		&worktreev1.WatchedWorktree{Dir: "/trees/broken", RepoDir: "/var/folders/T/repo"},
		&worktreev1.WatchedWorktree{Dir: "/trees/live", RepoDir: "/home/h/komrad", Branch: "task/live", Readable: true},
		&worktreev1.WatchedWorktree{Dir: "/trees/orphan", RepoDir: "/home/h/komrad", Branch: "task/orphan", Readable: true},
	}
}
