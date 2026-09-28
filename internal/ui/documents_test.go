package ui_test

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const documentsScreen = "the documents"

// TestDocumentsOpen covers the screen opening on the documents of the row the cursor was on.
func TestDocumentsOpen(t *testing.T) {
	t.Parallel()

	t.Run("an agent's row opens its own documents", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)

		frame := harness.readFrame()
		assert.Equal(t, documentsScreen, harness.readScreen())
		assert.Contains(t, readStageText(frame), "Design the cache")
		harness.assertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{AgentId: proto.String("author")})
	})

	t.Run("a row with none does not open it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildDocumentAuthors()...)
		moveToAgentRow(t, harness, "quiet")
		harness.press("space", "d")

		assert.NotEqual(t, documentsScreen, harness.readScreen())
		assert.Contains(t, readOpenMessages(harness), "this agent has not written any documents yet")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceListDocumentsProcedure))
	})

	t.Run("back returns to the agents list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.press("esc")

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(frame), "author")
	})
}

// TestDocumentsRead covers the document under the cursor drawn beside the list, read once and
// read again only when the agent writes.
func TestDocumentsRead(t *testing.T) {
	t.Parallel()

	t.Run("the first document is drawn as it opens", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "RFC, written")
		assert.Contains(t, stage, "Keep the cache warm.")
		assert.Equal(t, 1, strings.Count(stage, "Design the cache"), "a heading that repeats the name is drawn once")
	})

	t.Run("moving the cursor reads the next", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
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

		harness, docs := openDocuments(t)
		docs.rewrite("d1", "The cache is cold now.", "2026-09-26T12:00:00Z")

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = proto.String("2026-09-26T12:00:00Z")
		harness.watch(buildPuts(author))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListDocumentsProcedure), 2)
		assert.Contains(t, readStageText(harness.readFrame()), "The cache is cold now.")
	})

	t.Run("a rewrite within the second it was listed in reads again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		docs.rewrite("d1", "The cache is cold now.", "2026-09-25T12:00:00.5Z")

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = proto.String("2026-09-25T12:00:00.5Z")
		harness.watch(buildPuts(author))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceGetDocumentProcedure), 2)
		assert.Contains(t, readStageText(harness.readFrame()), "The cache is cold now.")
	})

	t.Run("another document stamped with the newest one's time lists again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		docs.add(&agentv1.Document{Id: "d3", Name: "Cache notes", Kind: agentv1.DocumentKind_DOCUMENT_KIND_NOTES, UpdatedAt: "2026-09-25T12:00:00Z"})

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.Documents = 3
		harness.watch(buildPuts(author))

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceListDocumentsProcedure), 2)
		assert.Contains(t, harness.readFrame(), "Cache notes")
	})
}

// TestDocumentsEdit covers opening the document under the cursor in your editor and saving what
// you wrote there.
func TestDocumentsEdit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		write     string
		refuse    bool
		between   func(*panelHarness, *documentShelf)
		close     func(*panelHarness)
		wantSaved string
		wantKept  bool
	}{
		{
			name:      "the key again saves what changed",
			write:     "# Design the cache\n\nKeep the cache cold.",
			close:     func(h *panelHarness) { h.press("e") },
			wantSaved: "# Design the cache\n\nKeep the cache cold.",
		},
		{
			name:  "the editor quitting saves what changed",
			write: "Rewritten.",
			close: func(h *panelHarness) {
				editors := h.panel.ReadRespawnPanes()
				h.panel.SetPaneDead(editors[len(editors)-1])
				h.fireTimers()
			},
			wantSaved: "Rewritten.",
		},
		{
			name:  "leaving the screen saves what changed",
			write: "Rewritten.",
			close: func(h *panelHarness) {
				h.press("esc")
				require.Equal(t, agentsScreen, h.readScreen())
			},
			wantSaved: "Rewritten.",
		},
		{
			name:      "a refused write keeps the copy",
			write:     "Rewritten.",
			refuse:    true,
			close:     func(h *panelHarness) { h.press("e") },
			wantSaved: "Rewritten.",
			wantKept:  true,
		},
		{
			name:  "a write refused because the agent rewrote it meanwhile keeps the copy",
			write: "Rewritten.",
			between: func(h *panelHarness, docs *documentShelf) {
				docs.rewrite("d1", "The agent's own rewrite.", "2026-09-26T12:00:00Z")
				author := buildDocumentAuthors()[2].(*agentv1.Agent)
				author.DocumentsUpdatedAt = proto.String("2026-09-26T12:00:00Z")
				h.watch(buildPuts(author))
			},
			refuse:    true,
			close:     func(h *panelHarness) { h.press("e") },
			wantSaved: "Rewritten.",
			wantKept:  true,
		},
		{
			name:  "nothing changed saves nothing",
			write: "# Design the cache\n\nKeep the cache warm.",
			close: func(h *panelHarness) { h.press("e") },
		},
		{
			name:  "an emptied document is not saved",
			write: "  \n",
			close: func(h *panelHarness) { h.press("e") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, docs := openDocuments(t)
			harness.answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				if tt.refuse {
					return nil, connect.NewError(connect.CodeFailedPrecondition, nil)
				}

				return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
			})
			harness.press("e")

			respawns := harness.panel.ReadRespawns()
			require.NotEmpty(t, respawns)
			path := readEditedPath(t, respawns[len(respawns)-1])
			assert.NotContains(t, readStageText(harness.readFrame()), "Keep the cache warm.", "the editor stands where the document was drawn")

			require.NoError(t, os.WriteFile(path, []byte(tt.write), 0o600))
			if tt.between != nil {
				tt.between(harness, docs)
			}
			tt.close(harness)

			if tt.wantKept {
				assert.FileExists(t, path)
			}

			if tt.wantSaved == "" {
				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceWriteDocumentProcedure))

				return
			}

			harness.assertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, &agentv1.WriteDocumentRequest{
				Id:      proto.String("d1"),
				Version: proto.Int32(3),
				Name:    "Design the cache",
				Kind:    agentv1.DocumentKind_DOCUMENT_KIND_RFC,
				Source:  &agentv1.WriteDocumentRequest_Body{Body: tt.wantSaved},
			})
			if !tt.wantKept {
				assert.NoFileExists(t, path)
			}
		})
	}
}

// TestDocumentsNew covers writing a new document for the agent whose documents are listed.
func TestDocumentsNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    *agentv1.WriteDocumentRequest
	}{
		{
			name:    "a name and the kind left alone write an RFC saying it is empty",
			presses: []string{"c", "P", "l", "a", "n", "ctrl+s"},
			want: &agentv1.WriteDocumentRequest{
				AgentId: proto.String("author"),
				Name:    "Plan",
				Kind:    agentv1.DocumentKind_DOCUMENT_KIND_RFC,
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty RFC document.\n"},
			},
		},
		{
			name:    "the kind chosen names what the document says it is",
			presses: []string{"c", "L", "o", "g", "tab", "down", "down", "down", "ctrl+s"},
			want: &agentv1.WriteDocumentRequest{
				AgentId: proto.String("author"),
				Name:    "Log",
				Kind:    agentv1.DocumentKind_DOCUMENT_KIND_NOTES,
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty notes document.\n"},
			},
		},
		{
			name:    "a blank name writes nothing",
			presses: []string{"c", " ", "ctrl+s"},
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

			if tt.want == nil {
				assert.Empty(t, harness.readSent(agentv1connect.AgentServiceWriteDocumentProcedure))

				return
			}
			harness.assertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, tt.want)
		})
	}
}

// TestDocumentsHistory covers stepping back and forward through a document's writes, drawing
// what each changed.
func TestDocumentsHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		presses  []string
		unlisted bool
		relisted bool
		want     []string
		wantNot  []string
	}{
		{
			name:    "stepping back draws what the newest write changed",
			presses: []string{"<"},
			want:    []string{"version 3 of 3, written by you", "3      -  Keep the cache cold.", "5      -  Evict nightly.", "3 +  Keep the cache warm."},
		},
		{
			name:    "stepping back again draws the write before it",
			presses: []string{"<", "<"},
			want:    []string{"version 2 of 3, written by the agent", "5 +  Evict nightly."},
			wantNot: []string{"Keep the cache warm."},
		},
		{
			name:    "the first write is drawn whole as added, and stepping past it stays there",
			presses: []string{"<", "<", "<", "<"},
			want:    []string{"version 1 of 3", "@@ -0,0 +1,3 @@", "3 +  Keep the cache cold."},
		},
		{
			name:    "stepping forward draws the later write",
			presses: []string{"<", "<", ">"},
			want:    []string{"version 3 of 3"},
		},
		{
			name:    "stepping forward past the newest draws the document as it stands",
			presses: []string{"<", ">"},
			want:    []string{"RFC, written", "Keep the cache warm."},
			wantNot: []string{"version 3 of 3"},
		},
		{
			name:    "moving the cursor leaves the writes behind",
			presses: []string{"<", "j"},
			want:    []string{"It went out on Tuesday."},
			wantNot: []string{"version"},
		},
		{
			name:    "stepping back through writes not kept from the first steps by what was kept",
			presses: []string{"j", "<"},
			want:    []string{"version 5 of 5", "1      -  It went out on Monday.", "1 +  It went out on Tuesday."},
		},
		{
			name:    "the oldest write kept when earlier ones were not is drawn as it is",
			presses: []string{"j", "<", "<"},
			want:    []string{"version 4 of 5", "the writes before it were not kept", "It went out on Monday."},
			wantNot: []string{"+  It went out on Monday."},
		},
		{
			name:    "panning slides the lines under the gutter",
			presses: []string{"<", "ctrl+a", "l"},
			want:    []string{"        3 +"},
			wantNot: []string{"Keep the cache warm."},
		},
		{
			name:    "panning waits for the diff to be focused",
			presses: []string{"<", "l"},
			want:    []string{"3 +  Keep the cache warm."},
		},
		{
			name:    "panning back to the start draws the lines whole",
			presses: []string{"<", "ctrl+a", "l", "0"},
			want:    []string{"3 +  Keep the cache warm."},
		},
		{
			name:    "stepping to another write starts its lines at the left",
			presses: []string{"<", "ctrl+a", "l", "<"},
			want:    []string{"version 2 of 3", "5 +  Evict nightly."},
		},
		{
			name:    "moving in the focused diff leaves the list where it is",
			presses: []string{"<", "ctrl+a", "j", "j"},
			want:    []string{"version 3 of 3"},
		},
		{
			name:    "leaving the diff gives the keys back to the list",
			presses: []string{"<", "ctrl+a", "esc", "j"},
			want:    []string{"It went out on Tuesday."},
			wantNot: []string{"version"},
		},
		{
			name:    "searching the diff counts what it found",
			presses: []string{"<", "ctrl+a", "/", "w", "a", "r", "m", "enter"},
			want:    []string{"/ warm", "1 of 1"},
		},
		{
			name:    "a match past the last is said to be missing",
			presses: []string{"<", "ctrl+a", "/", "z", "z", "enter", "n"},
			want:    []string{"no matches"},
		},
		{
			name:    "the diff keeps the keys across a write it cannot draw",
			presses: []string{"j", "<", "ctrl+a", "<", "k"},
			want:    []string{"version 4 of 5"},
		},
		{
			name:    "leaving a write it cannot draw stays on the screen",
			presses: []string{"j", "<", "ctrl+a", "<", "esc", ">"},
			want:    []string{"version 5 of 5"},
		},
		{
			name:    "a document drawn whole has no more to show",
			presses: []string{"<", "ctrl+a", "j", "E"},
			want:    []string{"there is no more of that file to show"},
		},
		{
			name:     "writes that could not be listed are listed again on the next step",
			presses:  []string{"<"},
			unlisted: true,
			relisted: true,
			want:     []string{"version 3 of 3"},
		},
		{
			name:     "writes that cannot be listed say so",
			presses:  []string{"<"},
			unlisted: true,
			want:     []string{"could not list its versions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, docs := openDocuments(t)
			docs.mu.Lock()
			docs.unlisted = tt.unlisted
			docs.mu.Unlock()

			for _, key := range tt.presses {
				harness.press(key)
				harness.fireTimers()
			}

			if tt.relisted {
				docs.mu.Lock()
				docs.unlisted = false
				docs.mu.Unlock()
				harness.press("<")
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

// TestDocumentsReading covers the document as it stands taking the keys, walked with a cursor
// and searched.
func TestDocumentsReading(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    []string
		wantNot []string
	}{
		{
			name:    "moving in the focused document leaves the list where it is",
			presses: []string{"ctrl+a", "j", "j"},
			want:    []string{"Keep the cache warm."},
			wantNot: []string{"It went out on Tuesday."},
		},
		{
			name:    "leaving the document gives the keys back to the list",
			presses: []string{"ctrl+a", "esc", "j"},
			want:    []string{"It went out on Tuesday."},
		},
		{
			name:    "searching the document lands on what it found and counts it",
			presses: []string{"ctrl+a", "/", "w", "a", "r", "m", "enter"},
			want:    []string{"/ warm", "1 of 1"},
		},
		{
			name:    "walking off the match counts every match instead",
			presses: []string{"ctrl+a", "/", "w", "a", "r", "m", "enter", "g", "g"},
			want:    []string{"/ warm", "1 match"},
			wantNot: []string{"1 of 1"},
		},
		{
			name:    "the next match wraps back round to the only one",
			presses: []string{"ctrl+a", "/", "w", "a", "r", "m", "enter", "g", "g", "n"},
			want:    []string{"1 of 1"},
		},
		{
			name:    "the previous match wraps back round to the only one",
			presses: []string{"ctrl+a", "/", "w", "a", "r", "m", "enter", "g", "g", "N"},
			want:    []string{"1 of 1"},
		},
		{
			name:    "a pattern nothing matches says so",
			presses: []string{"ctrl+a", "/", "z", "z", "enter", "n"},
			want:    []string{"no matches"},
		},
		{
			name:    "the next match with no search asks for one",
			presses: []string{"ctrl+a", "n"},
			want:    []string{"nothing to search for yet"},
		},
		{
			name:    "abandoning the search drops it",
			presses: []string{"ctrl+a", "/", "w", "a", "esc"},
			wantNot: []string{"/ wa"},
		},
		{
			name:    "stepping back from the focused document focuses the write's diff",
			presses: []string{"ctrl+a", "<", "j", "j"},
			want:    []string{"version 3 of 3"},
		},
		{
			name:    "stepping forward past the newest keeps the keys on the document",
			presses: []string{"ctrl+a", "<", ">", "j", "j"},
			want:    []string{"Keep the cache warm."},
			wantNot: []string{"version", "It went out on Tuesday."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, _ := openDocuments(t)
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

// TestDocumentsReadingCursor covers the cursor drawn in the document once it holds the keys, and
// the search's hits painted in it.
func TestDocumentsReadingCursor(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	line := regexp.MustCompile(`Keep the cache`)

	unfocused := findRawLine(harness.readRawFrame(), line)
	require.NotEmpty(t, unfocused)

	harness.press("ctrl+a", "G")
	focused := findRawLine(harness.readRawFrame(), line)
	require.NotEmpty(t, focused)
	assert.NotEqual(t, unfocused, focused, "the cursor is drawn on the line it moved to")

	harness.press("/", "w", "a", "r", "m", "enter")
	found := findRawLine(harness.readRawFrame(), line)
	require.NotEmpty(t, found)
	assert.NotEqual(t, focused, found, "the hit is painted")
}

// TestDocumentsReadingKeepsTheCursorShown covers the rows the search adds over a long document
// never pushing the cursor out of the window.
func TestDocumentsReadingKeepsTheCursorShown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
	}{
		{name: "opening the search", presses: []string{"/", "z", "z"}},
		{name: "a jump that finds nothing", presses: []string{"n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, docs := openDocuments(t)
			lines := make([]string, 0, 81)
			for range 80 {
				lines = append(lines, "Filler paragraph.\n")
			}
			docs.rewrite("d2", strings.Join(append(lines, "The last line."), "\n"), "2026-09-26T12:00:00Z")

			author := buildDocumentAuthors()[2].(*agentv1.Agent)
			author.DocumentsUpdatedAt = proto.String("2026-09-26T12:00:00Z")
			harness.watch(buildPuts(author))

			harness.press("j")
			harness.fireTimers()
			harness.press("ctrl+a", "G")
			require.Contains(t, readStageText(harness.readFrame()), "The last line.")

			harness.press(tt.presses...)
			assert.Contains(t, readStageText(harness.readFrame()), "The last line.")
		})
	}
}

// TestDocumentsDiffCursor covers the cursor in a version's diff, held dim while the list has the
// keys, lit once the diff is focused, and walking the lines from there.
func TestDocumentsDiffCursor(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	heading := regexp.MustCompile(`1\s+1\s+# Design the cache`)

	harness.press("<")
	harness.fireTimers()
	dim := findRawLine(harness.readRawFrame(), heading)
	require.NotEmpty(t, dim)

	harness.press("ctrl+a")
	live := findRawLine(harness.readRawFrame(), heading)
	require.NotEmpty(t, live)
	assert.NotEqual(t, dim, live, "focusing the diff lights the cursor")

	harness.press("j")
	plain := findRawLine(harness.readRawFrame(), heading)
	require.NotEmpty(t, plain)
	assert.NotEqual(t, live, plain, "the cursor leaves the line it stood on")
	assert.NotEqual(t, dim, plain)
}

// TestDocumentsDiffSearchOutlastsTheSplit covers the search prompt keeping its keys while the
// panel is too narrow to draw the diff beside the list.
func TestDocumentsDiffSearchOutlastsTheSplit(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	harness.press("<")
	harness.fireTimers()
	harness.press("ctrl+a", "/", "w")

	harness.send(tea.WindowSizeMsg{Width: 50, Height: 40})
	harness.press("a", "r", "m", "enter")
	harness.send(tea.WindowSizeMsg{Width: 140, Height: 40})

	stage := readStageText(harness.readFrame())
	assert.Contains(t, stage, "/ warm")
	assert.Contains(t, stage, "1 of 1")
}

// readEditedPath is the file an editor's shell line opens.
func readEditedPath(t *testing.T, respawn string) string {
	t.Helper()

	match := regexp.MustCompile(`'([^']*komrad-document-[^']*\.md)'`).FindStringSubmatch(respawn)
	require.Len(t, match, 2, "no document copy in %q", respawn)

	return match[1]
}

// openDocuments opens the documents screen from the agents list on the author, and fires the
// rest the cursor waits for.
func openDocuments(t *testing.T) (*panelHarness, *documentShelf) {
	t.Helper()

	docs := newDocumentShelf()
	harness := openAgents(t, buildDocumentAuthors()...)
	docs.answer(harness)

	moveToAgentRow(t, harness, "author")
	harness.press("space", "d")
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
	// writes is every write kept of a document, oldest first.
	writes map[string][]documentWrite
	// unlisted refuses to list the writes of a document.
	unlisted bool
}

// documentWrite is one write kept of a document.
type documentWrite struct {
	version  int32
	body     string
	byPerson bool
}

func newDocumentShelf() *documentShelf {
	return &documentShelf{
		listed: []*agentv1.Document{
			{Id: "d1", Name: "Design the cache", Kind: agentv1.DocumentKind_DOCUMENT_KIND_RFC, Version: 3, UpdatedAt: "2026-09-25T12:00:00Z"},
			{Id: "d2", Name: "Rollout report", Kind: agentv1.DocumentKind_DOCUMENT_KIND_REPORT, Version: 5, UpdatedAt: "2026-09-24T12:00:00Z"},
		},
		bodies: map[string]string{
			"d1": "# Design the cache\n\nKeep the cache warm.",
			"d2": "It went out on Tuesday.",
		},
		writes: map[string][]documentWrite{
			"d1": {
				{version: 1, body: "# Design the cache\n\nKeep the cache cold."},
				{version: 2, body: "# Design the cache\n\nKeep the cache cold.\n\nEvict nightly."},
				{version: 3, body: "# Design the cache\n\nKeep the cache warm.", byPerson: true},
			},
			"d2": {
				{version: 4, body: "It went out on Monday."},
				{version: 5, body: "It went out on Tuesday."},
			},
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
			document.Version++
			d.writes[id] = append(d.writes[id], documentWrite{version: document.GetVersion(), body: body})
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
			if read.Version != nil {
				whole.Version = read.GetVersion()
				for _, write := range d.writes[read.GetId()] {
					if write.version == read.GetVersion() {
						whole.Body = write.body
					}
				}
			}

			return connect.NewResponse(&agentv1.GetDocumentResponse{Document: whole}), nil
		}

		return nil, connect.NewError(connect.CodeNotFound, nil)
	})
	harness.answer(agentv1connect.AgentServiceListDocumentVersionsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		d.mu.Lock()
		defer d.mu.Unlock()

		if d.unlisted {
			return nil, connect.NewError(connect.CodeUnavailable, nil)
		}

		listed, _ := req.Any().(*agentv1.ListDocumentVersionsRequest)
		writes := d.writes[listed.GetId()]
		versions := make([]*agentv1.DocumentVersion, 0, len(writes))
		for i := len(writes) - 1; i >= 0; i-- {
			versions = append(versions, &agentv1.DocumentVersion{
				Version:   writes[i].version,
				ByPerson:  writes[i].byPerson,
				CreatedAt: "2026-09-25T12:00:00Z",
			})
		}

		return connect.NewResponse(&agentv1.ListDocumentVersionsResponse{Versions: versions}), nil
	})
}
