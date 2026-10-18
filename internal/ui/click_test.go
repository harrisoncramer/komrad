package ui_test

import (
	"fmt"
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

	t.Run("a click on the row under the cursor opens the menu", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.send(clickOnLine(t, harness, "└─ auth", "auth"))

		assert.Contains(t, harness.readFrame(), "edit files")
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
	harness := openCrowded(t, panelHarnessParams{})

	harness.send(clickOnLine(t, harness, "agents  schedules", "documents"))

	assert.Equal(t, documentsScreen, harness.readScreen())
}

// TestClickKind covers a left click on the row of kinds switching the list to the kind under it.
func TestClickKind(t *testing.T) {
	harness := openCrowded(t, panelHarnessParams{})

	harness.send(clickOnLine(t, harness, "F   live", "archived"))

	assert.Contains(t, harness.readRawFrame(), paint.Panel.Style(paint.TonePrimary).Render("archived"))
}

// TestClickTab covers a left click on the sort strip choosing the column under it.
func TestClickTab(t *testing.T) {
	harness := openAgents(t, buildSortable()...)

	harness.send(clickOnLine(t, harness, "f   ", "activity"))

	assert.Equal(t, []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ task-alpha", "│  ├─ " + reviewerMark + "bob", "│  └─ " + reviewerMark + "charlie", "└─ delta"}, readListRows(harness.readFrame()))
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

	t.Run("a click behind an open menu closes it and lands", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := clickOn(t, harness, "backend")
		harness.press("space")

		harness.send(click)

		frame := harness.readFrame()
		assert.NotContains(t, frame, "edit files")
		assert.Equal(t, "backend", readHoveredRow(frame))
	})

	t.Run("a click on the row under the cursor behind an open menu closes it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := clickOnLine(t, harness, "└─ auth", "auth")
		harness.press("space")

		harness.send(click)

		frame := harness.readFrame()
		assert.NotContains(t, frame, "edit files")
		assert.Equal(t, "└─ auth", readHoveredRow(frame))
	})
}

// TestClickMenu covers a left click on an open menu picking the entry under it.
func TestClickMenu(t *testing.T) {
	harness := openAgents(t, buildRecorded()...)
	harness.press("space")

	harness.send(clickOn(t, harness, "cancel"))

	assert.NotContains(t, harness.readFrame(), "edit files")
}
