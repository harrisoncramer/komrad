package ui_test

import (
	"testing"

	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const reviewScreen = "the review"

// TestPlaces covers the menu of an agent's places opened beside the agents list.
func TestPlaces(t *testing.T) {
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
			wantTitle:  "Places in quiet",
			wantCrumb:  "agents › quiet",
			wantCursor: "› logs",
		},
		{
			name:       "a workflow's row opens its orchestrator's",
			row:        "komrad",
			wantTitle:  "Places in orchestrator",
			wantCrumb:  "agents › orchestrator",
			wantCursor: "› documents",
		},
		{
			name:       "an agent with a checkout starts on review",
			row:        "author",
			wantTitle:  "Places in author",
			wantCrumb:  "agents › author",
			wantCursor: "› review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openPlaces(t, tt.row)

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantTitle)
			assert.Contains(t, frame, tt.wantCrumb)
			assert.Contains(t, frame, tt.wantCursor)
		})
	}

	t.Run("an entry with nothing behind it is gray", func(t *testing.T) {
		t.Parallel()

		harness := openPlaces(t, "quiet")

		frame := harness.readRawFrame()
		assert.Contains(t, frame, readOffEntry("review", "r"))
		assert.Contains(t, frame, readOffEntry("documents", "d"))
		assert.NotContains(t, frame, readOffEntry("logs", "l"))
	})

	t.Run("review answers its letter", func(t *testing.T) {
		t.Parallel()

		server := newReviewServer(t)
		harness := newPanelHarness(t, panelHarnessParams{git: server.repo})
		server.serve(harness)
		harness.watch(server.buildSnapshot())
		harness.press("space", "r")

		assert.Equal(t, reviewScreen, harness.readScreen())
	})

	t.Run("review on an agent with no checkout says why", func(t *testing.T) {
		t.Parallel()

		harness := openPlaces(t, "quiet")
		harness.press("r")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "this agent has no checkout of its own to read")
	})

	t.Run("logs closes the menu and stays on the list", func(t *testing.T) {
		t.Parallel()

		harness := openPlaces(t, "author")
		harness.press("l")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.NotContains(t, harness.readFrame(), "Places in author")
		harness.assertSent()
	})

	t.Run("esc closes the menu and takes the agent off the trail", func(t *testing.T) {
		t.Parallel()

		harness := openPlaces(t, "author")
		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "Places in author")
		assert.NotContains(t, frame, "agents › author")
	})

	t.Run("the legend offers it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildPlacesAgents()...)
		harness.press(widenListPresses...)
		moveToAgentRow(t, harness, "quiet")
		harness.press("?")

		assert.Contains(t, readLegend(harness.readFrame()), "places")
	})
}

// TestPlacesInArchive covers the menu opened beside the archive, and the way back to it.
func TestPlacesInArchive(t *testing.T) {
	t.Parallel()

	t.Run("review is gray, since archived work is not reviewed", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedPlaces(t)

		frame := harness.readFrame()
		assert.Contains(t, frame, "archive › author")
		assert.Contains(t, frame, "› documents")
		assert.Contains(t, harness.readRawFrame(), readOffEntry("review", "r"))
	})

	t.Run("documents open, and back returns to the archive", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedPlaces(t)
		harness.press("d")
		harness.fireTimers()

		require.Equal(t, documentsScreen, harness.readScreen())
		assert.Contains(t, harness.readFrame(), "archive › author")
		harness.assertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{AgentId: proto.String("author")})

		harness.press("esc")

		assert.Equal(t, archiveScreen, harness.readScreen())
	})
}

// openPlaces opens the places menu from the agents list on the row named.
func openPlaces(t *testing.T, row string) *panelHarness {
	t.Helper()

	harness := openAgents(t, buildPlacesAgents()...)
	moveToAgentRow(t, harness, row)
	harness.press("space")

	return harness
}

// openArchivedPlaces opens the places menu on an archived agent that has written documents.
func openArchivedPlaces(t *testing.T) *panelHarness {
	t.Helper()

	author := newArchivedAgent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = proto.String("2026-09-25T12:00:00Z")

	harness := openArchive(t, newLiveWorkflow("w1", "komrad"), author)
	newDocumentShelf().answer(harness)
	harness.press("j", "space")
	require.Contains(t, harness.readFrame(), "Places in author")

	return harness
}

func buildPlacesAgents() []proto.Message {
	rows := buildDocumentAuthors()
	author, _ := rows[2].(*agentv1.Agent)
	author.Cwd = "/trees/author"

	return rows
}

// readOffEntry is a submenu entry as it is drawn when there is nothing behind it, gray with its
// key underlined.
func readOffEntry(label, mark string) string {
	underlined := ansi.Style{}.Underline(true).String() + mark + ansi.Style{}.Underline(false).String() + label[len(mark):]

	return paint.Panel.Style(paint.ToneMuted).Render(underlined)
}
