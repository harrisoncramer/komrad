package ui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClickRow covers a left click putting the cursor on the list row under it.
func TestClickRow(t *testing.T) {
	t.Run("a click on a row puts the cursor on it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()))

		harness.Send(harness.ClickOn("backend"))

		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("a click on the row under the cursor opens the menu", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)

		harness.Send(harness.ClickOnLine("└─ auth", "auth"))

		assert.Contains(t, harness.Frame(), "edit files")
	})

	t.Run("a right click leaves the cursor where it was", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := harness.ClickOn("backend")
		click.Button = tea.MouseRight

		harness.Send(click)

		assert.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()))
	})
}

// TestClickView covers a left click on the switcher opening the view under it.
func TestClickView(t *testing.T) {
	harness := openCrowded(t, paneltest.Params{})

	harness.Send(harness.ClickOnLine("agents  schedules", "documents"))

	assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
}

// TestClickKind covers a left click on the row of kinds switching the list to the kind under it.
func TestClickKind(t *testing.T) {
	harness := openCrowded(t, paneltest.Params{})

	harness.Send(harness.ClickOnLine("F   live", "archived"))

	assert.Contains(t, harness.RawFrame(), paint.Panel.Style(paint.TonePrimary).Render("archived"))
}

// TestClickTab covers a left click on the sort strip choosing the column under it.
func TestClickTab(t *testing.T) {
	harness := openAgents(t, buildSortable()...)

	harness.Send(harness.ClickOnLine("f   ", "activity"))

	assert.Equal(t, []string{"gadgets", "└─ echo", "komrad", "├─ Zulu", "├─ task-alpha", "│  ├─ " + reviewerMark + "bob", "│  └─ " + reviewerMark + "charlie", "└─ delta"}, paneltest.ListRows(harness.Frame()))
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
		harness.Press("j")
		harness.Advance(time.Second)

		harness.Send(harness.ClickOn("line number 3"))
		harness.Press("G")

		assert.Contains(t, harness.Frame(), "line number 39")
	})

	t.Run("a click behind an open menu closes it and lands", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := harness.ClickOn("backend")
		harness.Press("space")

		harness.Send(click)

		frame := harness.Frame()
		assert.NotContains(t, frame, "edit files")
		assert.Equal(t, "backend", paneltest.HoveredRow(frame))
	})

	t.Run("a click on the row under the cursor behind an open menu closes it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := harness.ClickOnLine("└─ auth", "auth")
		harness.Press("space")

		harness.Send(click)

		frame := harness.Frame()
		assert.NotContains(t, frame, "edit files")
		assert.Equal(t, "└─ auth", paneltest.HoveredRow(frame))
	})
}

// TestClickMenu covers a left click on an open menu picking the entry under it.
func TestClickMenu(t *testing.T) {
	harness := openAgents(t, buildRecorded()...)
	harness.Press("space")

	harness.Send(harness.ClickOn("cancel"))

	assert.NotContains(t, harness.Frame(), "edit files")
}
