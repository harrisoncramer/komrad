package ui_test

import (
	"regexp"
	"strings"
	"testing"

	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const reviewScreen = "the review"

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
			name:       "an agent with nothing to review or read starts on logs",
			row:        "quiet",
			wantTitle:  "┃ quiet",
			wantCrumb:  "agents › quiet",
			wantCursor: "› logs",
		},
		{
			name:       "a workflow's row opens its orchestrator's under the workflow's name",
			row:        "komrad",
			wantTitle:  "┃ komrad",
			wantCrumb:  "agents › komrad",
			wantCursor: "› logs",
		},
		{
			name:       "an agent with a checkout starts on review",
			row:        "author",
			wantTitle:  "┃ author",
			wantCrumb:  "agents › author",
			wantCursor: "› review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openSubpages(t, tt.row)

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantTitle)
			assert.Contains(t, frame, tt.wantCrumb)
			assert.Contains(t, frame, tt.wantCursor)
		})
	}

	t.Run("an entry with nothing behind it is gray", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "quiet")

		frame := harness.readRawFrame()
		assert.Regexp(t, readOffEntry("review", "r"), frame)
		assert.Regexp(t, readOffEntry("documents", "d"), frame)
		assert.NotRegexp(t, readOffEntry("logs", "l"), frame)
	})

	t.Run("an orchestrator has no review or documents, which its workflow's agents hold", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "komrad")

		frame := readStageText(harness.readFrame())
		assert.NotContains(t, frame, "review")
		assert.NotContains(t, frame, "documents")
		assert.Contains(t, frame, "terminal")
	})

	t.Run("review answers its letter", func(t *testing.T) {
		t.Parallel()

		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{git: server.repo})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		harness.press("space")
		menuUp := len(harness.panel.ReadZooms())
		require.True(t, harness.panel.IsZoomed())

		harness.press("r")

		assert.Equal(t, reviewScreen, harness.readScreen())
		assert.NotContains(t, harness.panel.ReadZooms()[menuUp:], false, "the list is not drawn again on the way")
	})

	t.Run("review on an agent with no checkout says why", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "quiet")
		harness.press("r")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "this agent has no checkout of its own to read")
	})

	t.Run("esc closes the menu and leaves the trail on the row", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "author")
		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "┃ author")
		assert.Equal(t, "agents › author", readTrail(frame))
	})

	t.Run("the legend offers it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		harness.press(widenListPresses...)
		moveToAgentRow(t, harness, "quiet")
		harness.press("?")

		assert.Contains(t, readLegend(harness.readFrame()), "space menu")
	})
}

// TestSubpagesInArchive covers the menu opened beside the archive, and the way back to it.
func TestSubpagesInArchive(t *testing.T) {
	t.Parallel()

	t.Run("review is gray, since archived work is not reviewed", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)

		frame := harness.readFrame()
		assert.Contains(t, frame, "archive › author")
		assert.Contains(t, frame, "› documents")
		assert.Regexp(t, readOffEntry("review", "r"), harness.readRawFrame())
		assert.Regexp(t, readOffEntry("terminal", "t"), harness.readRawFrame())
	})

	t.Run("git holds rebase gray, since an archived branch is not rebased", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)
		harness.press("g")

		assert.Regexp(t, readOffEntry("rebase with base", "r"), harness.readRawFrame())

		harness.press("r")

		assert.Contains(t, readOpenMessages(harness), "an archived agent's branch is not rebased")
	})

	t.Run("documents open, and back returns to the archive", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)
		harness.press("d")
		harness.fireTimers()

		require.Equal(t, documentsScreen, harness.readScreen())
		assert.Contains(t, harness.readFrame(), "archive › author")
		harness.assertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{AgentId: xptr.Ptr("author")})

		harness.press("esc")

		assert.Equal(t, archiveScreen, harness.readScreen())
	})
}

// openSubpages opens the subpages menu from the agents list on the row named.
func openSubpages(t *testing.T, row string) *panelHarness {
	t.Helper()

	harness := openAgents(t, buildSubpagesAgents()...)
	moveToAgentRow(t, harness, row)
	harness.press("space")

	return harness
}

// openArchivedSubpages opens the subpages menu on an archived agent that has written documents.
func openArchivedSubpages(t *testing.T) *panelHarness {
	t.Helper()

	author := newArchivedAgent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = xptr.Ptr("2026-09-25T12:00:00Z")

	harness := openGroupedArchive(t, newLiveWorkflow("w1", "komrad"), author)
	newDocumentShelf().answer(harness)
	harness.press("j", "space")
	require.Contains(t, harness.readFrame(), "┃ author")

	return harness
}

func buildSubpagesAgents() []proto.Message {
	rows := buildDocumentAuthors()
	author, _ := rows[2].(*agentv1.Agent)
	author.Cwd = "/trees/author"

	return rows
}

// readOffEntry matches a submenu entry as it is drawn when there is nothing behind it, gray with
// its key right-aligned.
func readOffEntry(label, key string) *regexp.Regexp {
	gray, _, _ := strings.Cut(paint.Panel.Style(paint.ToneMuted).Render("x"), "x")

	return regexp.MustCompile(regexp.QuoteMeta(gray+label) + " +" + regexp.QuoteMeta(key))
}
