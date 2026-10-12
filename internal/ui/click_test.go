package ui_test

import (
	"slices"
	"strings"
	"testing"

	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClickRow covers a left click putting the cursor on the list row under it.
func TestClickRow(t *testing.T) {
	t.Run("a click on a row puts the cursor on it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))

		harness.send(clickOn(t, harness.readFrame(), "backend"))

		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("a click on a row hands the keys back from the record", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("pick:record")

		harness.send(clickOn(t, harness.readFrame(), "backend"))

		frame := harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "backend", readHoveredRow(frame))
	})

	t.Run("a click on a row hands the keys back from the switcher", func(t *testing.T) {
		harness := openAgents(t, buildWorktrees()...)
		harness.press("shift+tab")
		require.Contains(t, harness.readFrame(), "tab/S-tab switch")

		harness.send(clickOn(t, harness.readFrame(), "reviewer"))

		frame := harness.readFrame()
		assert.NotContains(t, frame, "tab/S-tab switch")
		assert.Contains(t, readHoveredRow(frame), "reviewer")
	})

	t.Run("a click on a row hands the keys back from the tabs", func(t *testing.T) {
		harness := openWorktrees(t)
		harness.press("f")
		require.Empty(t, readHoveredRow(harness.readFrame()))

		harness.send(clickOn(t, harness.readFrame(), "reviewer"))

		assert.Contains(t, readHoveredRow(harness.readFrame()), "reviewer")
	})

	t.Run("a click on a schedule puts the cursor on it", func(t *testing.T) {
		harness := openSchedules(t, newLiveWorkflow("w1", "api"), newSchedule("s1", "morning-ci"), newPausedSchedule("s2", "nightly"))
		require.Contains(t, readHoveredRow(harness.readFrame()), "nightly")

		harness.send(clickOn(t, harness.readFrame(), "morning-ci"))

		assert.Contains(t, readHoveredRow(harness.readFrame()), "morning-ci")
	})

	t.Run("a click on a record field hands the record the keys and puts its cursor there", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		fields := slices.DeleteFunc(readRecord(harness.readFrame()), func(line string) bool { return strings.TrimSpace(line) == "" })
		require.Greater(t, len(fields), 1)
		label := strings.Fields(fields[len(fields)-1])[0]

		harness.send(tea.MouseClickMsg{X: 2, Y: findRecordLine(harness.readFrame(), label), Button: tea.MouseLeft})

		assert.True(t, strings.HasPrefix(readRecordCursor(harness.readFrame()), label+" "))
	})

	t.Run("a right click leaves the cursor where it was", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := clickOn(t, harness.readFrame(), "backend")
		click.Button = tea.MouseRight

		harness.send(click)

		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))
	})
}

// TestClickView covers a left click on the switcher opening the view under it.
func TestClickView(t *testing.T) {
	tests := []struct {
		name       string
		press      []string
		view       string
		wantScreen string
	}{
		{
			name:       "a click on a view opens it",
			view:       "documents",
			wantScreen: documentsScreen,
		},
		{
			name:       "a click on a view closes the switcher it was picked from",
			press:      []string{"tab"},
			view:       "worktrees",
			wantScreen: worktreesScreen,
		},
		{
			name:       "a click on the view that is up keeps it",
			view:       "agents",
			wantScreen: agentsScreen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})
			harness.press(tt.press...)

			harness.send(clickOnStrip(t, harness, "agents  schedules", tt.view))

			assert.Equal(t, tt.wantScreen, harness.readScreen())
			assert.NotContains(t, harness.readFrame(), "tab/S-tab switch")
		})
	}
}

// TestClickKind covers a left click on the row of kinds switching the list to the kind under it.
func TestClickKind(t *testing.T) {
	lit := paint.Panel.Style(paint.TonePrimary)
	tests := []struct {
		name  string
		press []string
		kind  string
	}{
		{
			name: "a click on a kind switches the list to it",
			kind: "archived",
		},
		{
			name:  "a click on a kind hands the keys back from the row of kinds",
			press: []string{"F"},
			kind:  "archived",
		},
		{
			name: "a click on the kind listed keeps it",
			kind: "live",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})
			harness.press(tt.press...)

			harness.send(clickOnStrip(t, harness, "F   live", tt.kind))

			assert.Contains(t, harness.readRawFrame(), lit.Render(tt.kind))
			assert.NotContains(t, harness.readFrame(), "back to the list")
		})
	}
}

// TestClickTab covers a left click on the sort strip choosing the column under it.
func TestClickTab(t *testing.T) {
	tests := []struct {
		name   string
		clicks []string
		want   []string
	}{
		{
			name:   "a click on a column sorts by it",
			clicks: []string{"activity"},
			want:   []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ task-alpha", "│  ├─ bob", "│  └─ charlie", "└─ delta"},
		},
		{
			name:   "a second click on a column flips it",
			clicks: []string{"name", "name"},
			want:   []string{"gadgets", "└─ echo", "komrad", "├─ task-alpha", "│  ├─ bob", "│  └─ charlie", "├─ delta", "└─ Zulu"},
		},
		{
			name:   "a click on the column sorted by flips it",
			clicks: []string{"created"},
			want:   []string{"gadgets", "└─ echo", "komrad", "├─ task-alpha", "│  ├─ bob", "│  └─ charlie", "├─ Zulu", "└─ delta"},
		},
		{
			name:   "a click on the grouping flattens the list",
			clicks: []string{"workflow"},
			want:   []string{"delta", "Zulu", "alpha", "  └ charlie", "  └ bob", "echo"},
		},
		{
			name:   "a second click on the grouping brings the headings back",
			clicks: []string{"workflow", "flat"},
			want:   []string{"gadgets", "└─ echo", "komrad", "├─ delta", "├─ Zulu", "└─ task-alpha", "   ├─ charlie", "   └─ bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildSortable()...)

			for _, label := range tt.clicks {
				harness.send(clickOnStrip(t, harness, "f   ", label))
			}

			frame := harness.readFrame()
			assert.Equal(t, tt.want, readListRows(frame))
			assert.Contains(t, frame, "back to the list", "the strip holds the keys")
		})
	}
}

func clickOn(t *testing.T, frame, row string) tea.MouseClickMsg {
	t.Helper()

	for y, line := range splitLines(frame) {
		sidebar, _, _ := strings.Cut(line, "│")
		if slices.Contains(strings.Fields(sidebar), row) {
			return tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft}
		}
	}

	require.Failf(t, "no row in the frame", "%q", row)

	return tea.MouseClickMsg{}
}

func clickOnStrip(t *testing.T, harness *panelHarness, strip, label string) tea.MouseClickMsg {
	t.Helper()

	left, _ := harness.panel.Inside()
	for y, line := range splitLines(harness.readFrame()) {
		sidebar, _, _ := strings.Cut(line, "│")
		if !strings.HasPrefix(strings.TrimSpace(sidebar), strip) {
			continue
		}

		if at := strings.Index(sidebar, label); at >= 0 {
			return tea.MouseClickMsg{X: left + ansi.StringWidth(sidebar[:at]), Y: y, Button: tea.MouseLeft}
		}
	}

	require.Failf(t, "no label on the strip", "%q", label)

	return tea.MouseClickMsg{}
}
