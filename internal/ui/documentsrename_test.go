package ui_test

import (
	"slices"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

// TestDocumentsRename covers a document's name typed over its row in the list.
func TestDocumentsRename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    *agentv1.WriteDocumentRequest
	}{
		{
			name:    "a new name rewrites the document under it, keeping its text",
			presses: slices.Concat([]string{"r"}, spellKeys(" now"), []string{"enter"}),
			want: &agentv1.WriteDocumentRequest{
				Id:      xptr.Ptr("d1"),
				Version: xptr.Ptr[int32](3),
				Name:    "Design the cache now",
				Type:    "RFC",
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "# Design the cache\n\nKeep the cache warm."},
			},
		},
		{
			name:    "esc renames nothing",
			presses: slices.Concat([]string{"r"}, spellKeys(" now"), []string{"esc"}),
		},
		{
			name:    "the same name renames nothing",
			presses: []string{"r", "enter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, _ := openDocuments(t)
			harness.answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
			})
			harness.press(tt.presses...)

			assert.NotContains(t, harness.readFrame(), "▏", "the field has closed")
			if tt.want == nil {
				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceWriteDocumentProcedure))

				return
			}
			harness.assertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, tt.want)
		})
	}
}

// TestDocumentsRenameField covers the name being typed over its row, drawn where the name was.
func TestDocumentsRenameField(t *testing.T) {
	t.Parallel()

	t.Run("the field opens on the name the row had and takes every key", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.press(slices.Concat([]string{"r"}, spellKeys("jec"))...)

		frame := harness.readFrame()
		assert.Contains(t, readHoveredRow(frame), "Design the cachejec▏")
		assert.Contains(t, readLegend(frame), "enter/C-s save")
		assert.Equal(t, documentsScreen, harness.readScreen())
	})

	t.Run("esc hands the keys back to the list", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.press("r", "esc", "j")

		assert.Contains(t, readHoveredRow(harness.readFrame()), "Rollout report")
	})

	t.Run("the field opens while an agent's write is being read again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.hold(agentv1connect.AgentServiceGetDocumentProcedure)
		rewriteAsTheAgent(harness, docs)

		harness.press(slices.Concat([]string{"r"}, spellKeys(" now"))...)

		assert.Contains(t, readHoveredRow(harness.readFrame()), "Design the cache now▏")
	})

	t.Run("a document not read yet says so instead of opening the field", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.hold(agentv1connect.AgentServiceGetDocumentProcedure)
		docs.add(&agentv1.Document{Id: "d3", Name: "Notes", Type: "notes", Version: 1, UpdatedAt: "2026-09-20T12:00:00Z"})
		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = xptr.Ptr("2026-09-26T12:00:00Z")
		harness.watch(buildPuts(author))

		harness.press("G", "r", "x")

		assert.NotContains(t, readHoveredRow(harness.readFrame()), "▏")
		assert.Contains(t, readOpenMessages(harness), `"Notes" has not been read yet`)
	})

	t.Run("a document the agent rewrote while its name was typed is renamed at that write", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
		})
		harness.press(slices.Concat([]string{"r"}, spellKeys(" now"))...)

		rewriteAsTheAgent(harness, docs)
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, &agentv1.WriteDocumentRequest{
			Id:      xptr.Ptr("d1"),
			Version: xptr.Ptr[int32](4),
			Name:    "Design the cache now",
			Type:    "RFC",
			Source:  &agentv1.WriteDocumentRequest_Body{Body: "The cache is cold now."},
		})
	})
}

// rewriteAsTheAgent is the author rewriting the first document and the watch saying so.
func rewriteAsTheAgent(harness *panelHarness, docs *documentShelf) {
	docs.rewrite("d1", "The cache is cold now.", "2026-09-26T12:00:00Z")
	author := buildDocumentAuthors()[2].(*agentv1.Agent)
	author.DocumentsUpdatedAt = xptr.Ptr("2026-09-26T12:00:00Z")
	harness.watch(buildPuts(author))
}

