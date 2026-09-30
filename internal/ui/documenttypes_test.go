package ui_test

import (
	"os"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const documentTypesScreen = "the document types"

// TestDocumentTypesOpen covers the document types opening from the documents and from the menu,
// and leaving back to where they were opened from.
func TestDocumentTypesOpen(t *testing.T) {
	t.Parallel()

	t.Run("the documents open them on the first type with its context", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t)

		frame := harness.readFrame()
		assert.Equal(t, documentTypesScreen, harness.readScreen())
		assert.Equal(t, []string{"explainer", "notes", "report", "RFC"}, readListRows(frame))
		assert.Contains(t, readStageText(frame), "How to write an explainer.")
	})

	t.Run("back returns to the documents, and back again to the agents", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t)
		harness.press("esc")
		assert.Equal(t, documentsScreen, harness.readScreen())

		harness.press("esc")
		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("the menu opens them from a view and back returns to it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildDocumentAuthors()...)
		answerDocumentTypes(harness)
		harness.press("space", "y")
		assert.Equal(t, documentTypesScreen, harness.readScreen())

		harness.press("esc")
		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("a type put on the watch is drawn", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t)
		harness.watch(buildPuts(newDocumentType("t5", "brief")))

		assert.Contains(t, readListRows(harness.readFrame()), "brief")
	})
}

// TestDocumentTypesWrite covers defining, renaming and deleting a type from the screen.
func TestDocumentTypesWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		presses   []string
		procedure string
		want      proto.Message
	}{
		{
			name:      "a name and a context define a type",
			presses:   []string{"c", "b", "r", "i", "e", "f", "tab", "S", "h", "o", "r", "t", "ctrl+s"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
			want:      &agentv1.WriteDocumentTypeRequest{Name: "brief", Context: "Short"},
		},
		{
			name:      "a type with no context is not defined",
			presses:   []string{"c", "b", "ctrl+s"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
		},
		{
			name:      "a new name renames the type, keeping its context",
			presses:   []string{"r", "s", "ctrl+s"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
			want: &agentv1.WriteDocumentTypeRequest{
				Id:      xptr.Ptr("t3"),
				Version: xptr.Ptr[int32](2),
				Name:    "explainers",
				Context: "How to write an explainer.",
			},
		},
		{
			name:      "the same name renames nothing",
			presses:   []string{"r", "ctrl+s"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
		},
		{
			name:      "confirming the delete deletes the type",
			presses:   []string{"d", "y"},
			procedure: agentv1connect.AgentServiceDeleteDocumentTypeProcedure,
			want:      &agentv1.DeleteDocumentTypeRequest{Id: "t3"},
		},
		{
			name:      "declining the delete deletes nothing",
			presses:   []string{"d", "n"},
			procedure: agentv1connect.AgentServiceDeleteDocumentTypeProcedure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openDocumentTypes(t)
			harness.press(tt.presses...)
			harness.fireTimers()
			assert.NotContains(t, harness.readFrame(), "New document type", "the form is down")
			assert.NotContains(t, harness.readFrame(), "Rename", "the form is down")

			assert.Equal(t, documentTypesScreen, harness.readScreen())
			if tt.want == nil {
				assert.Empty(t, harness.readSent(tt.procedure))

				return
			}
			harness.assertSentTo(tt.procedure, tt.want)
		})
	}
}

// TestDocumentTypesEdit covers a type's context opened in your editor and saved when it closes.
func TestDocumentTypesEdit(t *testing.T) {
	t.Parallel()

	harness := openDocumentTypes(t)
	harness.press("e")

	respawns := harness.panel.ReadRespawns()
	require.NotEmpty(t, respawns)
	path := readEditedPath(t, respawns[len(respawns)-1])
	require.NoError(t, os.WriteFile(path, []byte("Explain it plainly."), 0o600))
	harness.press("e")

	harness.assertSentTo(agentv1connect.AgentServiceWriteDocumentTypeProcedure, &agentv1.WriteDocumentTypeRequest{
		Id:      xptr.Ptr("t3"),
		Version: xptr.Ptr[int32](2),
		Name:    "explainer",
		Context: "Explain it plainly.",
	})
}

// TestDocumentTypesHistory covers stepping back and forward through a type's writes.
func TestDocumentTypesHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    []string
		wantNot []string
	}{
		{
			name:    "stepping back draws what the newest write changed",
			presses: []string{"<"},
			want:    []string{"version 2 of 2, written", "1      -  How to write one.", "1 +  How to write an explainer."},
			wantNot: []string{"written by"},
		},
		{
			name:    "stepping back again draws the first write",
			presses: []string{"<", "<"},
			want:    []string{"version 1 of 2", "1 +  How to write one."},
		},
		{
			name:    "stepping forward past the newest draws the context as it stands",
			presses: []string{"<", ">"},
			want:    []string{"How to write an explainer."},
			wantNot: []string{"version 2 of 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openDocumentTypes(t)
			for _, key := range tt.presses {
				harness.press(key)
				harness.fireTimers()
			}

			stage := readStageText(harness.readFrame())
			for _, want := range tt.want {
				assert.Contains(t, stage, want)
			}
			for _, wantNot := range tt.wantNot {
				assert.NotContains(t, stage, wantNot)
			}
		})
	}
}

// openDocumentTypes opens the document types from the documents of the author.
func openDocumentTypes(t *testing.T) *panelHarness {
	t.Helper()

	harness, _ := openDocuments(t)
	answerDocumentTypes(harness)
	harness.press("t")
	harness.fireTimers()

	return harness
}

func newDocumentType(id, name string) *agentv1.DocumentType {
	article := "a"
	if name == "explainer" {
		article = "an"
	}

	return &agentv1.DocumentType{
		Id:        id,
		Name:      name,
		Context:   "How to write " + article + " " + name + ".",
		Version:   2,
		UpdatedAt: "2026-09-25T12:00:00Z",
	}
}

// answerDocumentTypes answers the writes to the types, and keeps two writes of each.
func answerDocumentTypes(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceWriteDocumentTypeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.WriteDocumentTypeResponse{}), nil
	})
	harness.answer(agentv1connect.AgentServiceDeleteDocumentTypeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&emptypb.Empty{}), nil
	})
	harness.answer(agentv1connect.AgentServiceListDocumentTypeVersionsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.ListDocumentTypeVersionsResponse{Versions: []*agentv1.DocumentTypeVersion{
			{Version: 2, CreatedAt: "2026-09-25T12:00:00Z"},
			{Version: 1, CreatedAt: "2026-09-24T12:00:00Z"},
		}}), nil
	})
	harness.answer(agentv1connect.AgentServiceGetDocumentTypeProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*agentv1.GetDocumentTypeRequest)
		for _, row := range buildDocumentAuthors() {
			documentType, ok := row.(*agentv1.DocumentType)
			if !ok || documentType.GetName() != read.GetName() {
				continue
			}
			if read.GetVersion() == 1 {
				documentType.Context, documentType.Version = "How to write one.", 1
			}

			return connect.NewResponse(&agentv1.GetDocumentTypeResponse{DocumentType: documentType}), nil
		}

		return nil, connect.NewError(connect.CodeNotFound, nil)
	})
}
