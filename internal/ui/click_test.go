package ui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestClickRow covers a left click putting the cursor on the list row under it.
func TestClickRow(t *testing.T) {
	t.Run("a click on a row puts the cursor on it", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRecorded()...)
		require.Equal(t, "└─ auth", paneltest.HoveredRow(harness.Frame()))

		harness.Send(harness.ClickOn("backend"))

		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("a click on the row under the cursor opens the menu", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRecorded()...)

		harness.Send(harness.ClickOnLine("└─ auth", "auth"))

		assert.Contains(t, harness.Frame(), "edit files")
	})

	t.Run("a right click leaves the cursor where it was", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRecorded()...)
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
	harness := paneltest.OpenAgents(t, buildSortable()...)

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
		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "backend"),
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
			paneltest.ArchivedAgent("a2", "billing", "w1"),
		)
		harness.AnswerLastScreens(map[string]string{"a1": strings.Join(lines, "\n"), "a2": strings.Join(lines, "\n")})
		harness.Press("j")
		harness.Advance(time.Second)

		harness.Send(harness.ClickOn("line number 3"))
		harness.Press("G")

		assert.Contains(t, harness.Frame(), "line number 39")
	})

	t.Run("a click behind an open menu closes it and lands", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRecorded()...)
		click := harness.ClickOn("backend")
		harness.Press("space")

		harness.Send(click)

		frame := harness.Frame()
		assert.NotContains(t, frame, "edit files")
		assert.Equal(t, "backend", paneltest.HoveredRow(frame))
	})

	t.Run("a click on the row under the cursor behind an open menu closes it", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRecorded()...)
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
	harness := paneltest.OpenAgents(t, buildRecorded()...)
	harness.Press("space")

	harness.Send(harness.ClickOn("cancel"))

	assert.NotContains(t, harness.Frame(), "edit files")
}

func buildSortable() []proto.Message {
	newSortable := func(name, workflowID string, created, changed int) *agentv1.Agent {
		agent := paneltest.AgentAt(name, workflowID, created)
		agent.StateChangedAt = timestamppb.New(time.Date(2026, 9, 20, 12, changed, 0, 0, time.UTC))

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
		paneltest.Workflow("w1", "komrad"),
		paneltest.Workflow("w2", "gadgets"),
		newSortable("delta", "w1", 30, 10),
		charlie,
		zulu,
		alpha,
		paneltest.TaskOf(alpha),
		bob,
		newSortable("echo", "w2", 0, 50),
	}
}

func buildRecorded() []proto.Message {
	agent := paneltest.AgentAt("auth", "w1", 0)
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
