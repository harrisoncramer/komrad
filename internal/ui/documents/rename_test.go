package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
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
			presses: slices.Concat([]string{"space", "r"}, strings.Split(" now", ""), []string{"enter"}),
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
			presses: slices.Concat([]string{"space", "r"}, strings.Split(" now", ""), []string{"esc"}),
		},
		{
			name:    "the same name renames nothing",
			presses: []string{"space", "r", "enter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, _ := openDocuments(t)
			harness.Answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
			})
			harness.Press(tt.presses...)

			assert.NotContains(t, harness.Frame(), "▏", "the field has closed")
			if tt.want == nil {
				assert.Empty(t, harness.Sent(agentv1connect.AgentServiceWriteDocumentProcedure))

				return
			}
			harness.AssertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, tt.want)
		})
	}
}

// TestDocumentsRenameField covers the user typing the name over its row, where the panel renders the name.
func TestDocumentsRenameField(t *testing.T) {
	t.Parallel()

	t.Run("the field opens on the name the row had and takes every key", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.Press(slices.Concat([]string{"space", "r"}, strings.Split("jec", ""))...)

		frame := harness.Frame()
		assert.Contains(t, paneltest.HoveredRow(frame), "Design the cachejec▏")
		assert.Contains(t, paneltest.Legend(frame), "enter save")
		assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
	})

	t.Run("esc hands the keys back to the list", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.Press("space", "r", "esc", "j")

		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "Rollout report")
	})

	t.Run("the field opens while an agent's write is being read again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.Hold(agentv1connect.AgentServiceGetDocumentProcedure)
		rewriteAsTheAgent(harness, docs)

		harness.Press(slices.Concat([]string{"space", "r"}, strings.Split(" now", ""))...)

		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "Design the cache now▏")
	})

	t.Run("a document not read yet says so instead of opening the field", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.Hold(agentv1connect.AgentServiceGetDocumentProcedure)
		docs.add(&agentv1.Document{
			Id:        "d3",
			AgentId:   "author",
			TaskId:    xptr.Ptr("task-author"),
			Name:      "Notes",
			Type:      "notes",
			Version:   1,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
		})
		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
		harness.Watch(paneltest.Puts(author))

		harness.Press("G", "space", "r", "x")

		assert.NotContains(t, paneltest.HoveredRow(harness.Frame()), "▏")
		assert.Contains(t, harness.ReadMessages(), `"Notes" has not been read yet`)
	})

	t.Run("a saved name stands until the listing shows it", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.Answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
		})
		harness.Press(slices.Concat([]string{"space", "r"}, strings.Split(" now", ""), []string{"enter"})...)
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "Design the cache now", "not the old name before the list is read again")

		docs.rename("d1", "Design the cache now", time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
		listAgain(harness, time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
		docs.rename("d1", "Design the cache later", time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC))
		listAgain(harness, time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC))
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "Design the cache later", "the list renders the name once it has caught up")
	})

	t.Run("a document the agent rewrote while its name was typed is renamed at that write", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		harness.Answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
		})
		harness.Press(slices.Concat([]string{"space", "r"}, strings.Split(" now", ""))...)

		rewriteAsTheAgent(harness, docs)
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, &agentv1.WriteDocumentRequest{
			Id:      xptr.Ptr("d1"),
			Version: xptr.Ptr[int32](4),
			Name:    "Design the cache now",
			Type:    "RFC",
			Source:  &agentv1.WriteDocumentRequest_Body{Body: "The cache is cold now."},
		})
	})
}

// listAgain is the watch saying the author wrote at the time given.
func listAgain(harness *paneltest.Panel, at time.Time) {
	author := buildDocumentAuthors()[2].(*agentv1.Agent)
	author.DocumentsUpdatedAt = dates.Stamp(at)
	harness.Watch(paneltest.Puts(author))
}

// rewriteAsTheAgent is the author rewriting the first document and the watch saying so.
func rewriteAsTheAgent(harness *paneltest.Panel, docs *documentShelf) {
	docs.rewrite("d1", "The cache is cold now.", time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
	listAgain(harness, time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
}
