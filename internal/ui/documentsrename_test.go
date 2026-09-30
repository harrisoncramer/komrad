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
}
