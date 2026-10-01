package ui_test

import (
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestDocumentTypesList covers the types listed from the panel's band of the menu, each drawn
// with its context and the menu key opening the ways to act on it in the context's place.
func TestDocumentTypesList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    []string
		wantNot []string
	}{
		{
			name: "the list draws every type and the context of the one under the cursor",
			want: []string{"explainer", "notes", "report", "RFC", "How to write an explainer."},
		},
		{
			name:    "moving down draws the next type's context",
			presses: []string{"down"},
			want:    []string{"How to write a notes."},
			wantNot: []string{"How to write an explainer."},
		},
		{
			name:    "the menu key draws the actions in the context's place",
			presses: []string{"space"},
			want:    []string{"edit", "new type", "delete"},
			wantNot: []string{"How to write an explainer."},
		},
		{
			name:    "back from the actions draws the context again",
			presses: []string{"space", "esc"},
			want:    []string{"How to write an explainer."},
			wantNot: []string{"new type"},
		},
		{
			name:    "the edit form takes the actions' place",
			presses: []string{"space", "e"},
			want:    []string{"Edit document type", "explainer"},
			wantNot: []string{"new type"},
		},
		{
			name:    "back from a form draws the actions again",
			presses: []string{"space", "e", "esc"},
			want:    []string{"new type"},
			wantNot: []string{"Edit document type"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openDocumentTypes(t)
			harness.press(tt.presses...)

			frame := harness.readFrame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, wantNot := range tt.wantNot {
				assert.NotContains(t, frame, wantNot)
			}
		})
	}

	t.Run("with no types the list says so and the actions only define one", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, newLiveWorkflow("w1", "api"), newSchedule("s1", "morning-ci"))
		harness.press("space", "T")
		assert.Contains(t, harness.readFrame(), "no document types")

		harness.press("space")

		raw := harness.readRawFrame()
		assert.Regexp(t, readOffEntry("edit", "e"), raw)
		assert.Regexp(t, readOffEntry("delete", "d"), raw)
		assert.NotRegexp(t, readOffEntry("new type", "c"), raw)
	})
}

// TestDocumentTypesWrite covers defining, editing and deleting a type from the menu on it.
func TestDocumentTypesWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		presses   []string
		procedure string
		want      proto.Message
	}{
		{
			name:      "a new type backed out of defines nothing",
			presses:   []string{"space", "c", "b", "esc", "esc", "esc", "esc"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
		},
		{
			name:      "declining the delete deletes nothing",
			presses:   []string{"space", "d", "n"},
			procedure: agentv1connect.AgentServiceDeleteDocumentTypeProcedure,
		},
		{
			name:      "a name and a context define a type",
			presses:   []string{"space", "c", "b", "r", "i", "e", "f", "tab", "S", "h", "o", "r", "t", "ctrl+s"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
			want:      &agentv1.WriteDocumentTypeRequest{Name: "brief", Context: "Short"},
		},
		{
			name:      "an edit writes the name and context over the type's version",
			presses:   []string{"space", "e", "s", "ctrl+s"},
			procedure: agentv1connect.AgentServiceWriteDocumentTypeProcedure,
			want: &agentv1.WriteDocumentTypeRequest{
				Id:      xptr.Ptr("t3"),
				Version: xptr.Ptr[int32](2),
				Name:    "explainers",
				Context: "How to write an explainer.",
			},
		},
		{
			name:      "confirming the delete deletes the type",
			presses:   []string{"space", "d", "y"},
			procedure: agentv1connect.AgentServiceDeleteDocumentTypeProcedure,
			want:      &agentv1.DeleteDocumentTypeRequest{Id: "t3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openDocumentTypes(t)
			harness.press(tt.presses...)
			harness.fireTimers()

			assert.Equal(t, agentsScreen, harness.readScreen())
			assert.NotContains(t, harness.readFrame(), "document type", "the form is down")
			if tt.want == nil {
				assert.Empty(t, harness.readSent(tt.procedure))

				return
			}
			harness.assertSentTo(tt.procedure, tt.want)
		})
	}
}

// TestDocumentTypesRefused covers a type the form cannot define.
func TestDocumentTypesRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    []string
	}{
		{
			name:    "a type with no context is refused",
			presses: []string{"space", "c", "b", "ctrl+s"},
			want:    []string{`Could not define document type "b"`, "a document type needs the context an agent reads"},
		},
		{
			name:    "a type with no name is refused without quoting one",
			presses: []string{"space", "c", "ctrl+s"},
			want:    []string{"Could not define a document type", "a document type needs a name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openDocumentTypes(t)
			harness.press(tt.presses...)
			harness.fireTimers()

			assert.Contains(t, harness.readFrame(), "New document type", "the form stays up")

			harness.press("ctrl+g")
			frame := harness.readFrame()
			for _, want := range append(tt.want, "fix and submit again") {
				assert.Contains(t, frame, want)
			}
			assert.Empty(t, harness.readSent(agentv1connect.AgentServiceWriteDocumentTypeProcedure))
		})
	}
}

// openDocumentTypes lists the document types from the menu over the agents list.
func openDocumentTypes(t *testing.T) *panelHarness {
	t.Helper()

	harness := openAgents(t, buildDocumentAuthors()...)
	answerDocumentTypes(harness)
	harness.press("space", "T")

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

// answerDocumentTypes answers the writes to the types.
func answerDocumentTypes(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceWriteDocumentTypeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.WriteDocumentTypeResponse{}), nil
	})
	harness.answer(agentv1connect.AgentServiceDeleteDocumentTypeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&emptypb.Empty{}), nil
	})
}
