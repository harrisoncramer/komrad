package ui_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClickRow covers a left click putting the cursor on the list row under it.
func TestClickRow(t *testing.T) {
	t.Run("a click on a row puts the cursor on it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))

		harness.send(clickOn(t, harness, "backend"))

		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("a click on a row hands the keys back from the record", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("pick:record")

		harness.send(clickOn(t, harness, "backend"))

		frame := harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "backend", readHoveredRow(frame))
	})

	t.Run("a click on a record field hands the record the keys and puts its cursor there", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		fields := slices.DeleteFunc(readRecord(harness.readFrame()), func(line string) bool { return strings.TrimSpace(line) == "" })
		require.Greater(t, len(fields), 1)
		label := strings.Fields(fields[len(fields)-1])[0]

		harness.send(clickOnLine(t, harness, fields[len(fields)-1], label))

		assert.True(t, strings.HasPrefix(readRecordCursor(harness.readFrame()), label+" "))
	})

	t.Run("a right click leaves the cursor where it was", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := clickOn(t, harness, "backend")
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})
			harness.press(tt.press...)

			harness.send(clickOnLine(t, harness, "agents  schedules", tt.view))

			assert.Equal(t, tt.wantScreen, harness.readScreen())
			assert.NotContains(t, harness.readFrame(), "tab/S-tab switch")
		})
	}
}

// TestClickKind covers a left click on the row of kinds switching the list to the kind under it.
func TestClickKind(t *testing.T) {
	lit := paint.Panel.Style(paint.TonePrimary)
	tests := []struct {
		name string
		kind string
	}{
		{
			name: "a click on a kind switches the list to it",
			kind: "archived",
		},
		{
			name: "a click on the kind listed keeps it",
			kind: "live",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})

			harness.send(clickOnLine(t, harness, "F   live", tt.kind))

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
			name:   "a click on the grouping flattens the list",
			clicks: []string{"workflow"},
			want:   []string{"delta", "Zulu", "alpha", "  └ charlie", "  └ bob", "echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, buildSortable()...)

			for _, label := range tt.clicks {
				harness.send(clickOnLine(t, harness, "f   ", label))
			}

			frame := harness.readFrame()
			assert.Equal(t, tt.want, readListRows(frame))
			assert.Contains(t, frame, "back to the list", "the strip holds the keys")
		})
	}
}

// TestClickBlock covers a left click that lands on no row handing the keys to the block under it.
func TestClickBlock(t *testing.T) {
	t.Run("a click in the column gives it the keys", func(t *testing.T) {
		lines := make([]string, 40)
		for i := range lines {
			lines[i] = fmt.Sprintf("line number %d", i)
		}
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgent("a1", "api-refactor", "w1"),
			newArchivedAgent("a2", "billing", "w1"),
		)
		answerLastScreens(harness, map[string]string{"a1": strings.Join(lines, "\n"), "a2": strings.Join(lines, "\n")})
		harness.press("j")
		harness.fireTimers()

		harness.send(clickOn(t, harness, "line number 3"))
		harness.press("G")

		assert.Contains(t, harness.readFrame(), "line number 39")
	})

	t.Run("a click on the rule of the record gives it the keys", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Empty(t, readRecordCursor(harness.readFrame()))

		harness.send(clickOn(t, harness, "Info"))

		assert.NotEmpty(t, readRecordCursor(harness.readFrame()))
	})

	t.Run("a click on a document line hands the document the keys with its cursor on that line", func(t *testing.T) {
		harness, _ := openDocuments(t)

		harness.send(clickOn(t, harness, "Keep the cache"))

		assert.Contains(t, findRawLine(harness.readRawFrame(), regexp.MustCompile(`Keep the cache`)), paint.BackgroundSequence(paint.Panel.Background(paint.ShadeSelection)))
	})

	t.Run("a click behind an open menu does nothing", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := clickOn(t, harness, "backend")
		harness.press("space")

		harness.send(click)
		harness.press("esc")

		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))
	})
}
