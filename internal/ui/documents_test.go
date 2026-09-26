package ui_test

import (
	"strings"
	"sync"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const documentsTitle = "Documents"

// TestDocumentsOpen covers the screen opening on the documents of the row the cursor was on.
func TestDocumentsOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		row       string
		wantAgent string
	}{
		{
			name:      "an agent's row opens its own documents",
			row:       "author",
			wantAgent: "author",
		},
		{
			name:      "a workflow's row opens its orchestrator's",
			row:       "komrad",
			wantAgent: "o1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, _ := openDocuments(t, tt.row)

			frame := harness.readFrame()
			assert.Contains(t, frame, documentsTitle)
			assert.Contains(t, readStageText(frame), "Design the cache")
			harness.assertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{AgentId: proto.String(tt.wantAgent)})
		})
	}

	t.Run("the legend offers it only on a row with documents", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildDocumentAuthors()...)
		harness.press(widenListPresses...)
		moveToAgentRow(t, harness, "author")
		harness.press("?")
		assert.Contains(t, readLegend(harness.readFrame()), "documents")

		moveToAgentRow(t, harness, "quiet")
		assert.NotContains(t, readLegend(harness.readFrame()), "documents")
	})

	t.Run("a row with none does not open it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildDocumentAuthors()...)
		moveToAgentRow(t, harness, "quiet")
		harness.press("i")

		assert.NotContains(t, harness.readFrame(), documentsTitle)
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceListDocumentsProcedure))
	})

	t.Run("back returns to the agents list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t, "author")
		harness.press("esc")

		frame := harness.readFrame()
		assert.Contains(t, frame, agentsTitle)
		assert.Contains(t, readHoveredRow(frame), "author")
	})
}

// TestDocumentsRead covers the document under the cursor drawn beside the list, read once and
// read again only when the agent writes.
func TestDocumentsRead(t *testing.T) {
	t.Parallel()

	t.Run("the first document is drawn as it opens", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t, "author")

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "RFC, written")
		assert.Contains(t, stage, "Keep the cache warm.")
		assert.Equal(t, 1, strings.Count(stage, "Design the cache"), "a heading that repeats the name is drawn once")
	})

	t.Run("moving the cursor reads the next", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t, "author")
		harness.press("j")
		harness.fireTimers()

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "Rollout report")
		assert.Contains(t, stage, "It went out on Tuesday.")

		harness.press("k")
		harness.fireTimers()
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceGetDocumentProcedure), 2, "a document already read is not read again")
	})

	t.Run("a new write lists and reads again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t, "author")
		docs.rewrite("d1", "The cache is cold now.", "2026-09-26T12:00:00Z")

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = proto.String("2026-09-26T12:00:00Z")
		harness.watch(buildPuts(author))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListDocumentsProcedure), 2)
		assert.Contains(t, readStageText(harness.readFrame()), "The cache is cold now.")
	})

	t.Run("a rewrite within the second it was listed in reads again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t, "author")
		docs.rewrite("d1", "The cache is cold now.", "2026-09-25T12:00:00.5Z")

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = proto.String("2026-09-25T12:00:00.5Z")
		harness.watch(buildPuts(author))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceGetDocumentProcedure), 2)
		assert.Contains(t, readStageText(harness.readFrame()), "The cache is cold now.")
	})

	t.Run("another document stamped with the newest one's time lists again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t, "author")
		docs.add(&agentv1.Document{Id: "d3", Name: "Cache notes", Kind: agentv1.DocumentKind_DOCUMENT_KIND_NOTES, UpdatedAt: "2026-09-25T12:00:00Z"})

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.Documents = 3
		harness.watch(buildPuts(author))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListDocumentsProcedure), 2)
		assert.Contains(t, harness.readFrame(), "Cache notes")
	})
}

// openDocuments opens the documents screen from the agents list on the row named, and fires the
// rest the cursor waits for.
func openDocuments(t *testing.T, row string) (*panelHarness, *documentShelf) {
	t.Helper()

	docs := newDocumentShelf()
	harness := openAgents(t, buildDocumentAuthors()...)
	docs.answer(harness)

	moveToAgentRow(t, harness, row)
	harness.press("i")
	harness.fireTimers()

	return harness, docs
}

// moveToAgentRow puts the agents list's cursor on the row whose name holds the one given.
func moveToAgentRow(t *testing.T, harness *panelHarness, name string) {
	t.Helper()

	harness.press("g", "g")
	for range 10 {
		if strings.Contains(readHoveredRow(harness.readFrame()), name) {
			return
		}
		harness.press("j")
	}

	require.Contains(t, readHoveredRow(harness.readFrame()), name, "no row is named %q", name)
}

// buildDocumentAuthors is a workflow whose orchestrator and one agent have written, beside an
// agent that has not.
func buildDocumentAuthors() []proto.Message {
	orchestrator := newOrchestrator()
	orchestrator.Documents = 2
	orchestrator.DocumentsUpdatedAt = proto.String("2026-09-25T12:00:00Z")

	author := newAgent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = proto.String("2026-09-25T12:00:00Z")

	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		orchestrator,
		author,
		newAgent("quiet", "quiet", "w1"),
	}
}

// documentShelf is what the service holds of every agent's documents, answering the list and
// the read.
type documentShelf struct {
	mu     sync.Mutex
	listed []*agentv1.Document
	bodies map[string]string
}

func newDocumentShelf() *documentShelf {
	return &documentShelf{
		listed: []*agentv1.Document{
			{Id: "d1", Name: "Design the cache", Kind: agentv1.DocumentKind_DOCUMENT_KIND_RFC, UpdatedAt: "2026-09-25T12:00:00Z"},
			{Id: "d2", Name: "Rollout report", Kind: agentv1.DocumentKind_DOCUMENT_KIND_REPORT, UpdatedAt: "2026-09-24T12:00:00Z"},
		},
		bodies: map[string]string{
			"d1": "# Design the cache\n\nKeep the cache warm.",
			"d2": "It went out on Tuesday.",
		},
	}
}

// rewrite changes a document's text and moves it to the top, as a write does.
func (d *documentShelf) rewrite(id, body, at string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.bodies[id] = body
	for _, document := range d.listed {
		if document.GetId() == id {
			document.UpdatedAt = at
		}
	}
}

// add is a document written beside the others.
func (d *documentShelf) add(document *agentv1.Document) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.listed = append(d.listed, document)
}

func (d *documentShelf) answer(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceListDocumentsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		d.mu.Lock()
		defer d.mu.Unlock()

		listed := make([]*agentv1.Document, 0, len(d.listed))
		for _, document := range d.listed {
			copied, ok := proto.Clone(document).(*agentv1.Document)
			require.True(harness.t, ok)
			listed = append(listed, copied)
		}

		return connect.NewResponse(&agentv1.ListDocumentsResponse{Documents: listed}), nil
	})
	harness.answer(agentv1connect.AgentServiceGetDocumentProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		d.mu.Lock()
		defer d.mu.Unlock()

		read, _ := req.Any().(*agentv1.GetDocumentRequest)
		for _, document := range d.listed {
			if document.GetId() != read.GetId() {
				continue
			}

			whole, ok := proto.Clone(document).(*agentv1.Document)
			require.True(harness.t, ok)
			whole.Body = d.bodies[read.GetId()]

			return connect.NewResponse(&agentv1.GetDocumentResponse{Document: whole}), nil
		}

		return nil, connect.NewError(connect.CodeNotFound, nil)
	})
}
