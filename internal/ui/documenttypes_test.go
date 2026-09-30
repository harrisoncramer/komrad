package ui_test

import (
	"os"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	watchv1 "komrad/proto/gen/watch/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const documentTypesScreen = "the document types"

// TestDocumentTypesOpen covers the types opening from the panel's band of the menu on any screen.
func TestDocumentTypesOpen(t *testing.T) {
	t.Parallel()

	t.Run("the menu opens them on the first type with its context", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t, "author")

		frame := harness.readFrame()
		assert.Equal(t, documentTypesScreen, harness.readScreen())
		assert.Equal(t, []string{"explainer", "notes", "report", "RFC"}, readListRows(frame))
		assert.Contains(t, readStageText(frame), "How to write an explainer.")
	})

	t.Run("an agent that has written nothing opens them too", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t, "quiet")

		assert.Equal(t, documentTypesScreen, harness.readScreen())
	})

	t.Run("back returns to the agents list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t, "author")
		harness.press("esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "author")
	})

	t.Run("a screen with no agent opens them and gets them back", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, newLiveWorkflow("w1", "api"), newSchedule("s1", "morning-ci"))
		answerDocumentTypes(harness)
		harness.press("space", "T", "l")
		harness.fireTimers()

		assert.Equal(t, documentTypesScreen, harness.readScreen())

		harness.press("esc")

		assert.Equal(t, schedulesScreen, harness.readScreen())
	})

	t.Run("back returns to the documents they were opened over, which still go back to their own", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		answerDocumentTypes(harness)
		harness.press("space", "T", "l")
		harness.fireTimers()
		harness.press("esc")
		harness.fireTimers()

		assert.Equal(t, documentsScreen, harness.readScreen())
		assert.Contains(t, readStageText(harness.readFrame()), "Design the cache")

		harness.press("esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("back from over a review whose agent has gone lands on the agents list", func(t *testing.T) {
		t.Parallel()

		harness := openReview(t, newReviewServer(t))
		answerDocumentTypes(harness)
		harness.press("space", "T", "l")
		harness.fireTimers()
		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a1"))
		harness.press("esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("a type put on the watch is drawn", func(t *testing.T) {
		t.Parallel()

		harness := openDocumentTypes(t, "author")
		harness.watch(buildPuts(newDocumentType("t5", "brief")))

		assert.Contains(t, readListRows(harness.readFrame()), "brief")
	})
}

// TestDocumentTypesNewFromTheMenu covers defining a type without opening the types.
func TestDocumentTypesNewFromTheMenu(t *testing.T) {
	t.Parallel()

	harness := openAgents(t, buildDocumentAuthors()...)
	answerDocumentTypes(harness)
	moveToAgentRow(t, harness, "quiet")
	harness.press("space", "T", "c", "b", "r", "i", "e", "f", "tab", "S", "h", "o", "r", "t", "ctrl+s")

	assert.Equal(t, agentsScreen, harness.readScreen())
	harness.assertSentTo(agentv1connect.AgentServiceWriteDocumentTypeProcedure, &agentv1.WriteDocumentTypeRequest{Name: "brief", Context: "Short"})
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

			harness := openDocumentTypes(t, "author")
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

	harness := openDocumentTypes(t, "author")
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
			name:    "a write that renamed the type says what it was named",
			presses: []string{"<"},
			want:    []string{`renamed from "explanation"`},
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

			harness := openDocumentTypes(t, "author")
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

// openDocumentTypes opens the document types from the menu over the agent named.
func openDocumentTypes(t *testing.T, name string) *panelHarness {
	t.Helper()

	harness := openAgents(t, buildDocumentAuthors()...)
	answerDocumentTypes(harness)
	moveToAgentRow(t, harness, name)
	harness.press("space", "T", "l")
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

// answerDocumentTypes answers the writes to the types, and keeps two writes of each, the second of
// the explainer's renaming it.
func answerDocumentTypes(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceWriteDocumentTypeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.WriteDocumentTypeResponse{}), nil
	})
	harness.answer(agentv1connect.AgentServiceDeleteDocumentTypeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&emptypb.Empty{}), nil
	})
	harness.answer(agentv1connect.AgentServiceListDocumentTypeVersionsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		listed, _ := req.Any().(*agentv1.ListDocumentTypeVersionsRequest)
		name := ""
		for _, row := range buildDocumentAuthors() {
			if documentType, ok := row.(*agentv1.DocumentType); ok && documentType.GetId() == listed.GetId() {
				name = documentType.GetName()
			}
		}
		first := name
		if name == "explainer" {
			first = "explanation"
		}

		return connect.NewResponse(&agentv1.ListDocumentTypeVersionsResponse{Versions: []*agentv1.DocumentTypeVersion{
			{Version: 2, Name: name, CreatedAt: "2026-09-25T12:00:00Z"},
			{Version: 1, Name: first, CreatedAt: "2026-09-24T12:00:00Z"},
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
