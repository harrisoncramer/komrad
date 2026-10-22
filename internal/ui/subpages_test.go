package ui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestSubpages covers the menu of an agent's subpages opened beside the agents list.
func TestSubpages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		row        string
		wantTitle  string
		wantCrumb  string
		wantCursor string
	}{
		{
			name:       "an agent's row starts on logs",
			row:        "reviewer",
			wantTitle:  "  reviewer",
			wantCrumb:  "agents › reviewer",
			wantCursor: "› logs",
		},
		{
			name:       "a task with nothing to review starts on logs",
			row:        "task-quiet",
			wantTitle:  "  task-quiet",
			wantCrumb:  "agents › task-quiet",
			wantCursor: "› logs",
		},
		{
			name:       "a workflow's row opens its orchestrator's under the workflow's name",
			row:        "komrad",
			wantTitle:  "  komrad",
			wantCrumb:  "agents › komrad",
			wantCursor: "› logs",
		},
		{
			name:       "a task with a checkout starts on review",
			row:        "task-author",
			wantTitle:  "  task-author",
			wantCrumb:  "agents › task-author",
			wantCursor: "› review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openSubpages(t, tt.row)

			frame := harness.Frame()
			assert.Contains(t, frame, tt.wantTitle)
			assert.Contains(t, frame, tt.wantCrumb)
			assert.Contains(t, frame, tt.wantCursor)
		})
	}

	t.Run("an entry with nothing behind it is gray", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "task-quiet")

		frame := harness.RawFrame()
		assert.Regexp(t, readOffEntry("review", "r"), frame)
		assert.NotRegexp(t, readOffEntry("logs", "l"), frame)
	})

	t.Run("an orchestrator has no review", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "komrad")

		frame := paneltest.StageText(harness.Frame())
		assert.NotContains(t, frame, "review")
		assert.Contains(t, frame, "terminal")
	})

	t.Run("review answers its letter", func(t *testing.T) {
		t.Parallel()

		server := newReviewServer(t)
		harness := paneltest.Start(t, paneltest.Params{Git: server.repo})
		server.serve(harness)
		harness.Watch(server.buildSnapshot())
		harness.Press("space")
		menuUp := len(harness.Tmux().Zooms())
		require.True(t, harness.Tmux().IsZoomed())

		harness.Press("r")

		assert.Equal(t, screen.Review, paneltest.Screen(harness.Frame()))
		assert.NotContains(t, harness.Tmux().Zooms()[menuUp:], false, "the list is not rendered again on the way")
	})

	t.Run("review on a task with no checkout says why", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "task-quiet")
		harness.Press("r")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, readOpenMessages(harness), "this agent has no checkout of its own to read")
	})

	t.Run("esc closes the menu and leaves the trail on the row", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "task-author")
		harness.Press("esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, "› review")
		assert.Equal(t, "agents › task-author", paneltest.Trail(frame))
	})

	t.Run("the legend offers it", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.Press(widenListPresses...)
		harness.MoveToRow("task-quiet")
		harness.Press("?")

		assert.Regexp(t, `space +menu`, paneltest.Legend(harness.Frame()))
	})
}

// TestSubpagesInArchive covers the menu opened beside the archive, and the way back to it.
func TestSubpagesInArchive(t *testing.T) {
	t.Parallel()

	t.Run("an archived agent has no review", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)

		frame := harness.Frame()
		assert.Contains(t, frame, "agents › author")
		assert.Contains(t, frame, "› logs")
		assert.NotContains(t, paneltest.StageText(frame), "review")
		assert.Regexp(t, readOffEntry("terminal", "t"), harness.RawFrame())

		harness.Press("r")

		assert.Contains(t, harness.Frame(), "› logs")
		assert.NotContains(t, readOpenMessages(harness), "Could not open")
	})

	t.Run("git holds rebase gray, since an archived worktree is not touched", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)
		harness.Press("g")

		assert.Regexp(t, readOffEntry("rebase with base", "r"), harness.RawFrame())

		harness.Press("r")

		assert.Contains(t, readOpenMessages(harness), "komrad leaves an archived task's worktree alone")
	})

	t.Run("an archived agent on a live task has no git, since the checkout is not its own", func(t *testing.T) {
		t.Parallel()

		author := newArchivedAgent("author", "author", "w1")
		harness := openArchivedAgents(t, paneltest.Workflow("w1", "komrad"), author, paneltest.TaskOf(author))
		harness.Press("j", "j", "space")
		require.Contains(t, harness.Frame(), "  author")

		stage := paneltest.StageText(harness.Frame())
		require.Contains(t, stage, "jump")
		assert.NotContains(t, stage, "git")
	})

	t.Run("documents open filtered to the archived agent", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)
		harness.Press("J", "d")
		harness.Advance(time.Second)

		require.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.Equal(t, "▽ agent:author", readSearch(harness.Frame()))
		harness.AssertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{
			Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](100)},
		})
	})
}

// openSubpages opens the subpages menu from the agents list on the row named.
func openSubpages(t *testing.T, row string) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
	harness.MoveToRow(row)
	harness.Press("space")

	return harness
}

// openArchivedSubpages opens the subpages menu on an archived agent that has written documents.
func openArchivedSubpages(t *testing.T) *paneltest.Panel {
	t.Helper()

	author := newArchivedAgent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = xptr.Ptr("2026-09-25T12:00:00Z")
	task := paneltest.TaskOf(author)
	task.ArchivedAt = xptr.Ptr(archivedAt)

	harness := openArchivedAgents(t, paneltest.Workflow("w1", "komrad"), author, task)
	newDocumentShelf().answer(harness)
	harness.Press("j", "j", "space")
	require.Contains(t, harness.Frame(), "  author")

	return harness
}

func buildSubpagesAgents() []proto.Message {
	rows := buildDocumentAuthors()
	author, _ := rows[2].(*agentv1.Agent)
	author.Cwd = "/trees/author"

	return append(rows, paneltest.Reviewer("reviewer", "reviewer", "author"))
}

// readOffEntry matches a submenu entry as it is rendered when there is nothing behind it, gray with
// its key right-aligned.
func readOffEntry(label, key string) *regexp.Regexp {
	gray, _, _ := strings.Cut(paint.Panel.Style(paint.ToneMuted).Render("x"), "x")

	return regexp.MustCompile(regexp.QuoteMeta(gray+label) + " +" + regexp.QuoteMeta(key))
}
