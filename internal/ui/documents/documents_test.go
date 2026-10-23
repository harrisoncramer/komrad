package ui_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"
	taskv1 "komrad/proto/gen/task/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestDocumentsOpen covers the screen opening on the documents of the row the cursor was on.
func TestDocumentsOpen(t *testing.T) {
	t.Parallel()

	t.Run("a task's row opens every document filtered to the task's own", func(t *testing.T) {
		t.Parallel()

		docs := newDocumentShelf()
		docs.add(&agentv1.Document{
			Id:        "d3",
			AgentId:   "quiet",
			TaskId:    xptr.Ptr("task-quiet"),
			Name:      "Quiet notes",
			Type:      "notes",
			Version:   1,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
		})
		harness := paneltest.OpenAgents(t, buildDocumentAuthors()...)
		docs.answer(harness)
		harness.MoveToRow("task-author")
		harness.Press("space", "J", "d")
		harness.Advance(time.Second)

		frame := harness.Frame()
		assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.Equal(t, "▽ task:task-author", paneltest.Search(frame))
		assert.Contains(t, paneltest.StageText(frame), "Design the cache")
		assert.NotContains(t, strings.Join(paneltest.ListRows(frame), "\n"), "Quiet notes")
		harness.AssertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{
			Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](100)},
		})
	})

	t.Run("a workflow's row opens its orchestrator's documents under the workflow's name", func(t *testing.T) {
		t.Parallel()

		docs := newDocumentShelf()
		docs.add(&agentv1.Document{
			Id:        "d3",
			AgentId:   "o1",
			Name:      "Workflow plan",
			Type:      "notes",
			Version:   1,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
		})
		harness := paneltest.OpenAgents(t, buildDocumentAuthors()...)
		docs.answer(harness)
		harness.MoveToRow("komrad")
		harness.Press("space", "J", "d")
		harness.Advance(time.Second)

		frame := harness.Frame()
		rows := strings.Join(paneltest.ListRows(frame), "\n")
		assert.Equal(t, "▽ agent:komrad", paneltest.Search(frame))
		assert.Contains(t, rows, "Workflow plan")
		assert.NotContains(t, rows, "Design the cache")
	})

	t.Run("a new document from a task's documents is written by the agent its row wears", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.Answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
		})

		harness.Press("space", "c", "P", "enter")

		harness.AssertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, &agentv1.WriteDocumentRequest{
			AgentId: xptr.Ptr("author"),
			Name:    "P",
			Type:    "RFC",
			Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty RFC document.\n"},
		})
	})

	t.Run("a task's row opens only its own documents beside a task whose slug it prefixes", func(t *testing.T) {
		t.Parallel()

		docs := newDocumentShelf()
		docs.add(&agentv1.Document{
			Id:        "d3",
			AgentId:   "a1",
			TaskId:    xptr.Ptr("t1"),
			Name:      "Login plan",
			Type:      "RFC",
			Version:   1,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
		})
		docs.add(&agentv1.Document{
			Id:        "d4",
			AgentId:   "a3",
			TaskId:    xptr.Ptr("t2"),
			Name:      "Solo notes",
			Type:      "notes",
			Version:   1,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)),
		})
		rows := buildTasks()
		rows[2].(*taskv1.Task).Slug = "fix-login-2"
		harness := paneltest.OpenAgents(t, rows...)
		docs.answer(harness)
		harness.Press("j")
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "fix-login")
		require.NotContains(t, paneltest.HoveredRow(harness.Frame()), "fix-login-2")
		harness.Press("space", "J", "d")
		harness.Advance(time.Second)

		listed := strings.Join(paneltest.ListRows(harness.Frame()), "\n")
		assert.Contains(t, listed, "Login plan")
		assert.NotContains(t, listed, "Solo notes")
		assert.NotContains(t, listed, "Design the cache")
	})

	t.Run("a row with none opens it empty", func(t *testing.T) {
		t.Parallel()

		docs := newDocumentShelf()
		harness := paneltest.OpenAgents(t, buildDocumentAuthors()...)
		docs.answer(harness)
		harness.MoveToRow("task-quiet")
		harness.Press("space", "J", "d")
		harness.Advance(time.Second)

		assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, paneltest.StageText(harness.Frame()), "no documents")
	})

	t.Run("back clears the agent's filter and stays on the documents", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.Press("esc")

		assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.NotContains(t, paneltest.Search(harness.Frame()), "agent:author")
	})

	t.Run("closing the menu comes back to the same agent's documents", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.Press("space", "esc")

		frame := harness.Frame()
		assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, paneltest.StageText(frame), "Design the cache")
	})

	t.Run("the switcher opens every agent's documents, the hovered one's writer in its info", func(t *testing.T) {
		t.Parallel()

		harness := openEveryDocument(t)

		frame := harness.Frame()
		assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.NotContains(t, paneltest.HoveredRow(frame), "author")
		assert.Equal(t, "RFC", paneltest.RecordField(frame, "type"))
		assert.Contains(t, paneltest.RecordField(frame, "updated"), "Sep 25")
		assert.Equal(t, "author", paneltest.RecordField(frame, "agent"))
		harness.AssertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{
			Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](100)},
		})
	})
}

// TestDocumentsFilter covers the qualifiers the documents filter reads.
func TestDocumentsFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantNames  []string
		wantSearch string
	}{
		{
			name:       "agent: keeps the documents the agent wrote",
			query:      "agent:quiet",
			wantNames:  []string{"Quiet notes"},
			wantSearch: "▽ agent:quiet",
		},
		{
			name:       "a negated agent: is a problem on the line and takes nothing off",
			query:      "-agent:quiet",
			wantNames:  []string{"Design the cache", "Rollout report", "Quiet notes"},
			wantSearch: "▽ -agent:quiet  agent: cannot be negated",
		},
		{
			name:       "workflow: keeps the documents written in it",
			query:      "workflow:komrad",
			wantNames:  []string{"Design the cache", "Rollout report", "Quiet notes"},
			wantSearch: "▽ workflow:komrad",
		},
		{
			name:       "workflow: naming none keeps nothing",
			query:      "workflow:elsewhere",
			wantSearch: "▽ workflow:elsewhere",
		},
		{
			name:       "type: keeps the documents of the type",
			query:      "type:rfc",
			wantNames:  []string{"Design the cache"},
			wantSearch: "▽ type:rfc",
		},
		{
			name:       "since: keeps the documents written that day or after",
			query:      "since:2026-09-24",
			wantNames:  []string{"Design the cache", "Rollout report"},
			wantSearch: "▽ since:2026-09-24",
		},
		{
			name:       "until: keeps the documents written that day or before",
			query:      "until:2026-09-24",
			wantNames:  []string{"Rollout report", "Quiet notes"},
			wantSearch: "▽ until:2026-09-24",
		},
		{
			name:       "a day that is not one is a problem on the line and takes nothing off",
			query:      "since:soon",
			wantNames:  []string{"Design the cache", "Rollout report", "Quiet notes"},
			wantSearch: "▽ since:soon  since:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			docs := newDocumentShelf()
			docs.add(&agentv1.Document{
				Id:        "d3",
				AgentId:   "quiet",
				Name:      "Quiet notes",
				Type:      "notes",
				Version:   1,
				UpdatedAt: timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
			})
			harness := paneltest.OpenAgents(t, buildDocumentAuthors()...)
			docs.answer(harness)
			harness.MoveToRow("task-quiet")
			harness.Press("tab", "tab")
			harness.Advance(time.Second)

			harness.Press(slices.Concat([]string{"/", "tab"}, strings.Split(tt.query, ""), []string{"enter"})...)

			frame := harness.Frame()
			rows := strings.Join(paneltest.ListRows(frame), "\n")
			var names []string
			for _, name := range []string{"Design the cache", "Rollout report", "Quiet notes"} {
				if strings.Contains(rows, name) {
					names = append(names, name)
				}
			}
			assert.Equal(t, tt.wantNames, names)
			assert.True(t, strings.HasPrefix(paneltest.Search(frame), tt.wantSearch), paneltest.Search(frame))
		})
	}
}

// TestDocumentsSays covers the says: qualifier answered by a search of the documents' text on the server.
func TestDocumentsSays(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	var asked []string
	harness.Answer(agentv1connect.AgentServiceSearchDocumentsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		asked = append(asked, req.Any().(*agentv1.SearchDocumentsRequest).GetQuery())

		return connect.NewResponse(&agentv1.SearchDocumentsResponse{Matches: []*agentv1.DocumentHit{
			{DocumentId: "d2", Snippet: "out on \x02Tuesday\x03"},
		}}), nil
	})
	line := regexp.MustCompile(`It went out on`)
	marked := regexp.MustCompile(`\x1b\[[0-9;]*mTuesday`)
	picked := regexp.MustCompile(`38;5;208mRollout report`)

	harness.Press(slices.Concat([]string{"esc", "/", "tab"}, strings.Split("says:tuesday", ""))...)
	harness.Advance(time.Second)
	harness.Press("enter")
	harness.Advance(time.Second)

	frame := harness.Frame()
	assert.Equal(t, []string{`"tuesday"`}, asked)
	assert.Equal(t, []string{"Rollout report"}, paneltest.ListRows(frame))
	assert.Equal(t, "▽ says:tuesday", paneltest.Search(frame))
	assert.Regexp(t, marked, paneltest.FindRawLine(harness.RawFrame(), line), "the word found is lit in the document")
	assert.Regexp(t, picked, paneltest.RawListRow(harness.RawFrame(), "Rollout report"), "the document found is lit in the list")

	harness.Press("/", "esc")
	harness.Advance(time.Second)

	assert.NotRegexp(t, marked, paneltest.FindRawLine(harness.RawFrame(), line), "the word is dark once the qualifier is gone")
	assert.NotRegexp(t, picked, paneltest.RawListRow(harness.RawFrame(), "Rollout report"), "the document is dark once the qualifier is gone")
}

// TestDocumentsRead covers the document under the cursor rendered beside the list, read once and
// read again only when the agent writes.
func TestDocumentsRead(t *testing.T) {
	t.Parallel()

	t.Run("the first document is rendered as it opens", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)

		stage := paneltest.StageText(harness.Frame())
		assert.Contains(t, stage, "RFC, written")
		assert.Contains(t, stage, "Keep the cache warm.")
		assert.Equal(t, 1, strings.Count(stage, "Design the cache"), "a heading that repeats the name is rendered once")
	})

	t.Run("moving the cursor reads the next", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)
		harness.Press("j")
		harness.Advance(time.Second)

		stage := paneltest.StageText(harness.Frame())
		assert.Contains(t, stage, "Rollout report")
		assert.Contains(t, stage, "It went out on Tuesday.")

		harness.Press("k")
		harness.Advance(time.Second)
		assert.Len(t, harness.Sent(agentv1connect.AgentServiceGetDocumentProcedure), 2, "a document already read is not read again")
	})

	t.Run("a new write lists and reads again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		docs.rewrite("d1", "The cache is cold now.", time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
		harness.Watch(paneltest.Puts(author))

		assert.Len(t, harness.Sent(agentv1connect.AgentServiceListDocumentsProcedure), 2)
		assert.Contains(t, paneltest.StageText(harness.Frame()), "The cache is cold now.")
	})

	t.Run("a rewrite within the second it was listed in reads again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		docs.rewrite("d1", "The cache is cold now.", time.Date(2026, 9, 25, 12, 0, 0, 500_000_000, time.UTC))

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 500_000_000, time.UTC))
		harness.Watch(paneltest.Puts(author))

		assert.Len(t, harness.Sent(agentv1connect.AgentServiceGetDocumentProcedure), 2)
		assert.Contains(t, paneltest.StageText(harness.Frame()), "The cache is cold now.")
	})

	t.Run("another document stamped with the newest one's time lists again", func(t *testing.T) {
		t.Parallel()

		harness, docs := openDocuments(t)
		docs.add(&agentv1.Document{
			Id:        "d3",
			AgentId:   "author",
			TaskId:    xptr.Ptr("task-author"),
			Name:      "Cache notes",
			Type:      "notes",
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
		})

		author := buildDocumentAuthors()[2].(*agentv1.Agent)
		author.Documents = 3
		harness.Watch(paneltest.Puts(author))

		assert.Len(t, harness.Sent(agentv1connect.AgentServiceListDocumentsProcedure), 2)
		assert.Contains(t, harness.Frame(), "Cache notes")
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
		between   func(*paneltest.Panel, *documentShelf)
		close     func(*paneltest.Panel)
		wantSaved string
		wantKept  bool
	}{
		{
			name:      "the key again saves what changed",
			write:     "# Design the cache\n\nKeep the cache cold.",
			close:     func(h *paneltest.Panel) { h.Press("space", "e") },
			wantSaved: "# Design the cache\n\nKeep the cache cold.",
		},
		{
			name:  "the editor quitting saves what changed",
			write: "Rewritten.",
			close: func(h *paneltest.Panel) {
				editors := h.Tmux().RespawnPanes()
				h.Tmux().SetPaneDead(editors[len(editors)-1])
				h.Advance(time.Second)
			},
			wantSaved: "Rewritten.",
		},
		{
			name:  "leaving the screen saves what changed",
			write: "Rewritten.",
			close: func(h *paneltest.Panel) {
				h.Press("tab")
				require.Equal(t, screen.Worktrees, paneltest.Screen(h.Frame()))
			},
			wantSaved: "Rewritten.",
		},
		{
			name:      "a refused write keeps the copy",
			write:     "Rewritten.",
			refuse:    true,
			close:     func(h *paneltest.Panel) { h.Press("space", "e") },
			wantSaved: "Rewritten.",
			wantKept:  true,
		},
		{
			name:  "a write refused because the agent rewrote it meanwhile keeps the copy",
			write: "Rewritten.",
			between: func(h *paneltest.Panel, docs *documentShelf) {
				docs.rewrite("d1", "The agent's own rewrite.", time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
				author := buildDocumentAuthors()[2].(*agentv1.Agent)
				author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
				h.Watch(paneltest.Puts(author))
			},
			refuse:    true,
			close:     func(h *paneltest.Panel) { h.Press("space", "e") },
			wantSaved: "Rewritten.",
			wantKept:  true,
		},
		{
			name:  "nothing changed saves nothing",
			write: "# Design the cache\n\nKeep the cache warm.",
			close: func(h *paneltest.Panel) { h.Press("space", "e") },
		},
		{
			name:  "an emptied document is not saved",
			write: "  \n",
			close: func(h *paneltest.Panel) { h.Press("space", "e") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, docs := openDocuments(t)
			harness.Answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				if tt.refuse {
					return nil, connect.NewError(connect.CodeFailedPrecondition, nil)
				}

				return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
			})
			harness.Press("space", "e")

			respawns := harness.Tmux().Respawns()
			require.NotEmpty(t, respawns)
			path := readEditedPath(t, respawns[len(respawns)-1])
			assert.NotContains(t, paneltest.StageText(harness.Frame()), "Keep the cache warm.", "the editor stands where the document was rendered")

			require.NoError(t, os.WriteFile(path, []byte(tt.write), 0o600))
			if tt.between != nil {
				tt.between(harness, docs)
			}
			tt.close(harness)

			if tt.wantKept {
				assert.FileExists(t, path)
			}

			if tt.wantSaved == "" {
				assert.Empty(t, harness.Sent(agentv1connect.AgentServiceWriteDocumentProcedure))

				return
			}

			harness.AssertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, &agentv1.WriteDocumentRequest{
				Id:      xptr.Ptr("d1"),
				Version: xptr.Ptr[int32](3),
				Name:    "Design the cache",
				Type:    "RFC",
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
			name:    "a name and the type left alone write an RFC saying it is empty",
			presses: []string{"space", "c", "P", "l", "a", "n", "enter"},
			want: &agentv1.WriteDocumentRequest{
				AgentId: xptr.Ptr("author"),
				Name:    "Plan",
				Type:    "RFC",
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty RFC document.\n"},
			},
		},
		{
			name:    "the type chosen names what the document says it is",
			presses: []string{"space", "c", "L", "o", "g", "tab", "up", "up", "enter"},
			want: &agentv1.WriteDocumentRequest{
				AgentId: xptr.Ptr("author"),
				Name:    "Log",
				Type:    "notes",
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty notes document.\n"},
			},
		},
		{
			name:    "a blank name writes nothing",
			presses: []string{"space", "c", " ", "enter"},
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

			if tt.want == nil {
				assert.Empty(t, harness.Sent(agentv1connect.AgentServiceWriteDocumentProcedure))

				return
			}
			harness.AssertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, tt.want)
		})
	}
}

// TestDocumentsNewRerendersTheTypes covers the new document form offering a type defined while it is
// open.
func TestDocumentsNewRerendersTheTypes(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	harness.Press("space", "c")
	require.NotContains(t, harness.Frame(), "brief")

	harness.Watch(paneltest.Puts(newDocumentType("t5", "brief")))

	assert.Contains(t, harness.Frame(), "brief")
}

// TestDocumentsNewFromEmpty covers writing a first document for an agent that has none, from its
// empty documents view.
func TestDocumentsNewFromEmpty(t *testing.T) {
	t.Parallel()

	harness := paneltest.OpenAgents(t, buildDocumentAuthors()...)
	newDocumentShelf().answer(harness)
	harness.Answer(agentv1connect.AgentServiceWriteDocumentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
	})
	harness.MoveToRow("task-quiet")
	harness.Press("space", "J", "d")
	harness.Advance(time.Second)
	harness.Press("space", "c", "P", "l", "a", "n", "enter")

	assert.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
	harness.AssertSentTo(agentv1connect.AgentServiceWriteDocumentProcedure, &agentv1.WriteDocumentRequest{
		AgentId: xptr.Ptr("quiet"),
		Name:    "Plan",
		Type:    "RFC",
		Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty RFC document.\n"},
	})
}

// TestDocumentsMenu covers the menu space opens on a document or a type, over the documents view's own.
func TestDocumentsMenu(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		open     func(*testing.T) *paneltest.Panel
		presses  []string
		wantOn   []*regexp.Regexp
		wantOff  []*regexp.Regexp
		wantGone []*regexp.Regexp
	}{
		{
			name: "a document's band is titled for it and has no delete",
			open: func(t *testing.T) *paneltest.Panel {
				harness, _ := openDocuments(t)

				return harness
			},
			presses:  []string{"space"},
			wantOn:   []*regexp.Regexp{regexp.MustCompile(`Design the cache`), regexp.MustCompile(`edit +e`), regexp.MustCompile(`rename +r`), regexp.MustCompile(`new document +c`)},
			wantGone: []*regexp.Regexp{regexp.MustCompile(`delete +d`)},
		},
		{
			name: "an open editor turns edit into closing it and grays the rest",
			open: func(t *testing.T) *paneltest.Panel {
				harness, _ := openDocuments(t)

				return harness
			},
			presses: []string{"space", "e", "space"},
			wantOn:  []*regexp.Regexp{regexp.MustCompile(`close editor +e`)},
			wantOff: []*regexp.Regexp{paneltest.OffEntry("rename", "r"), paneltest.OffEntry("new document", "c")},
		},
		{
			name:    "every agent's documents have no agent to write a new one for",
			open:    openEveryDocument,
			presses: []string{"space"},
			wantOn:  []*regexp.Regexp{regexp.MustCompile(`edit +e`)},
			wantOff: []*regexp.Regexp{paneltest.OffEntry("new document", "c")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := tt.open(t)
			harness.Press(tt.presses...)

			frame := harness.Frame()
			for _, want := range tt.wantOn {
				assert.Regexp(t, want, frame)
			}
			for _, want := range tt.wantOff {
				assert.Regexp(t, want, harness.RawFrame())
			}
			for _, gone := range tt.wantGone {
				assert.NotRegexp(t, gone, frame)
			}
		})
	}
}

// TestDocumentsHistory covers stepping back and forward through a document's writes, rendering
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
		note     string
	}{
		{
			name:    "stepping back renders what the newest write changed",
			presses: []string{"<"},
			want:    []string{"version 3 of 3, written by you", "3      -  Keep the cache cold.", "5      -  Evict nightly.", "3 +  Keep the cache warm."},
		},
		{
			name:    "stepping back again renders the write before it",
			presses: []string{"<", "<"},
			want:    []string{"version 2 of 3, written by the agent", "5 +  Evict nightly."},
			wantNot: []string{"Keep the cache warm."},
		},
		{
			name:    "the first write is rendered whole as added, and stepping past it stays there",
			presses: []string{"<", "<", "<", "<"},
			want:    []string{"version 1 of 3", "@@ -0,0 +1,3 @@", "3 +  Keep the cache cold."},
		},
		{
			name:    "stepping forward renders the later write",
			presses: []string{"<", "<", ">"},
			want:    []string{"version 3 of 3"},
		},
		{
			name:    "stepping forward past the newest renders the document as it stands",
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
			name:    "the oldest write kept when earlier ones were not is rendered as it is",
			presses: []string{"j", "<", "<"},
			want:    []string{"version 4 of 5", "the writes before it were not kept", "It went out on Monday."},
			wantNot: []string{"+  It went out on Monday."},
		},
		{
			name:    "panning slides the lines under the gutter",
			presses: []string{"<", "pick:block", "l"},
			want:    []string{"        3 +"},
			wantNot: []string{"Keep the cache warm."},
		},
		{
			name:    "panning waits for the diff to be focused",
			presses: []string{"<", "l"},
			want:    []string{"3 +  Keep the cache warm."},
		},
		{
			name:    "panning back to the start renders the lines whole",
			presses: []string{"<", "pick:block", "l", "0"},
			want:    []string{"3 +  Keep the cache warm."},
		},
		{
			name:    "stepping to another write starts its lines at the left",
			presses: []string{"<", "pick:block", "l", "<"},
			want:    []string{"version 2 of 3", "5 +  Evict nightly."},
		},
		{
			name:    "moving in the focused diff leaves the list where it is",
			presses: []string{"<", "pick:block", "j", "j"},
			want:    []string{"version 3 of 3"},
		},
		{
			name:    "leaving the diff gives the keys back to the list",
			presses: []string{"<", "pick:block", "esc", "j"},
			want:    []string{"It went out on Tuesday."},
			wantNot: []string{"version"},
		},
		{
			name:    "searching the diff counts what it found",
			presses: []string{"<", "pick:block", "/", "w", "a", "r", "m", "enter"},
			want:    []string{"/ warm", "1 of 1"},
		},
		{
			name:    "a match past the last is said to be missing",
			presses: []string{"<", "pick:block", "/", "z", "z", "enter", "n"},
			note:    "no matches",
		},
		{
			name:    "the diff keeps the keys across a write it cannot render",
			presses: []string{"j", "<", "pick:block", "<", "k"},
			want:    []string{"version 4 of 5"},
		},
		{
			name:    "leaving a write it cannot render stays on the screen",
			presses: []string{"j", "<", "pick:block", "<", "esc", ">"},
			want:    []string{"version 5 of 5"},
		},
		{
			name:    "a document rendered whole has no more to show",
			presses: []string{"<", "pick:block", "j", "E"},
			note:    "there is no more of that file to show",
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
				harness.Press(key)
				harness.Advance(time.Second)
			}

			if tt.relisted {
				docs.mu.Lock()
				docs.unlisted = false
				docs.mu.Unlock()
				harness.Press("<")
				harness.Advance(time.Second)
			}

			stage := paneltest.StageText(harness.Frame())
			for _, want := range tt.want {
				assert.Contains(t, stage, want)
			}
			for _, wantNot := range tt.wantNot {
				assert.NotContains(t, stage, wantNot)
			}
			if tt.note != "" {
				assert.Contains(t, harness.ReadMessages(), tt.note)
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
			presses: []string{"pick:block", "j", "j"},
			want:    []string{"Keep the cache warm."},
			wantNot: []string{"It went out on Tuesday."},
		},
		{
			name:    "leaving the document gives the keys back to the list",
			presses: []string{"pick:block", "esc", "j"},
			want:    []string{"It went out on Tuesday."},
		},
		{
			name:    "searching the document lands on what it found and counts it",
			presses: []string{"pick:block", "/", "w", "a", "r", "m", "enter"},
			want:    []string{"/ warm", "1 of 1"},
		},
		{
			name:    "walking off the match counts every match instead",
			presses: []string{"pick:block", "/", "w", "a", "r", "m", "enter", "g", "g"},
			want:    []string{"/ warm", "1 match"},
			wantNot: []string{"1 of 1"},
		},
		{
			name:    "the next match wraps back round to the only one",
			presses: []string{"pick:block", "/", "w", "a", "r", "m", "enter", "g", "g", "n"},
			want:    []string{"1 of 1"},
		},
		{
			name:    "the previous match wraps back round to the only one",
			presses: []string{"pick:block", "/", "w", "a", "r", "m", "enter", "g", "g", "N"},
			want:    []string{"1 of 1"},
		},
		{
			name:    "a pattern nothing matches says so",
			presses: []string{"pick:block", "/", "z", "z", "enter", "n"},
			want:    []string{"no matches"},
		},
		{
			name:    "a broken pattern counts nothing while it is typed",
			presses: []string{"pick:block", "/", "w", "a", "r", "m", "("},
			want:    []string{"/ warm(", "0 matches"},
		},
		{
			name:    "the next match with no search asks for one",
			presses: []string{"pick:block", "n"},
			want:    []string{"nothing to search for yet"},
		},
		{
			name:    "abandoning the search drops it",
			presses: []string{"pick:block", "/", "w", "a", "esc"},
			wantNot: []string{"/ wa"},
		},
		{
			name:    "stepping back from the focused document focuses the write's diff",
			presses: []string{"pick:block", "<", "j", "j"},
			want:    []string{"version 3 of 3"},
		},
		{
			name:    "stepping forward past the newest keeps the keys on the document",
			presses: []string{"pick:block", "<", ">", "j", "j"},
			want:    []string{"Keep the cache warm."},
			wantNot: []string{"version", "It went out on Tuesday."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness, _ := openDocuments(t)
			for _, key := range tt.presses {
				harness.Press(key)
				harness.Advance(time.Second)
			}

			stage := paneltest.StageText(harness.Frame())
			for _, want := range tt.want {
				assert.Contains(t, stage, want)
			}
			for _, wantNot := range tt.wantNot {
				assert.NotContains(t, stage, wantNot)
			}
		})
	}

	t.Run("the focused document quits on ctrl+c", func(t *testing.T) {
		t.Parallel()

		harness, _ := openDocuments(t)

		harness.Press("pick:block", "ctrl+c")

		assert.Equal(t, 1, harness.Tmux().Detaches())
	})
}

// TestDocumentsReadingCursor covers the cursor rendered in the document once it holds the keys, and
// the search's hits painted in it.
func TestDocumentsReadingCursor(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	line := regexp.MustCompile(`Keep the cache`)

	bar := paint.BackgroundSequence(paint.Panel.Background(paint.ShadeSelection))

	assert.NotEqual(t, "/", strings.TrimSpace(paneltest.Stage(harness.Frame())[0]), "the search waits for the document to hold the keys")
	unfocused := paneltest.FindRawLine(harness.RawFrame(), line)
	require.NotEmpty(t, unfocused)
	assert.NotContains(t, unfocused, bar)

	harness.Press("pick:block", "G")
	assert.Equal(t, "/", strings.TrimSpace(paneltest.Stage(harness.Frame())[0]), "the search stands over the focused document")
	focused := paneltest.FindRawLine(harness.RawFrame(), line)
	require.NotEmpty(t, focused)
	assert.Contains(t, focused, bar, "the cursor is rendered as a bar on the line it moved to")

	harness.Press("/", "w", "a", "r", "m", "enter")
	found := paneltest.FindRawLine(harness.RawFrame(), line)
	require.NotEmpty(t, found)
	assert.NotEqual(t, focused, found, "the hit is painted")
	hit := strings.Index(found, "warm")
	require.GreaterOrEqual(t, hit, 0)
	assert.Contains(t, found[hit:], bar, "the bar carries on past the hit")
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
			docs.rewrite("d2", strings.Join(append(lines, "The last line."), "\n"), time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))

			author := buildDocumentAuthors()[2].(*agentv1.Agent)
			author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
			harness.Watch(paneltest.Puts(author))

			harness.Press("j")
			harness.Advance(time.Second)
			harness.Press("pick:block", "G")
			require.Contains(t, paneltest.StageText(harness.Frame()), "The last line.")

			harness.Press(tt.presses...)
			assert.Contains(t, paneltest.StageText(harness.Frame()), "The last line.")
		})
	}
}

// TestDocumentsDiffCursor covers the cursor in a version's diff, held dim while the list has the
// keys, lit once the diff is focused, and walking the lines from there.
func TestDocumentsDiffCursor(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	heading := regexp.MustCompile(`1\s+1\s+# Design the cache`)

	harness.Press("<")
	harness.Advance(time.Second)
	dim := paneltest.FindRawLine(harness.RawFrame(), heading)
	require.NotEmpty(t, dim)

	harness.Press("pick:block")
	live := paneltest.FindRawLine(harness.RawFrame(), heading)
	require.NotEmpty(t, live)
	assert.NotEqual(t, dim, live, "focusing the diff lights the cursor")

	harness.Press("j")
	plain := paneltest.FindRawLine(harness.RawFrame(), heading)
	require.NotEmpty(t, plain)
	assert.NotEqual(t, live, plain, "the cursor leaves the line it stood on")
	assert.NotEqual(t, dim, plain)
}

// TestDocumentsDiffSearchOutlastsTheSplit covers the search prompt keeping its keys while the
// panel is too narrow to render the diff beside the list.
func TestDocumentsDiffSearchOutlastsTheSplit(t *testing.T) {
	t.Parallel()

	harness, _ := openDocuments(t)
	harness.Press("<")
	harness.Advance(time.Second)
	harness.Press("pick:block", "/", "w")

	harness.Send(tea.WindowSizeMsg{Width: 50, Height: 40})
	harness.Press("a", "r", "m", "enter")
	harness.Send(tea.WindowSizeMsg{Width: 140, Height: 40})

	stage := paneltest.StageText(harness.Frame())
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
func openDocuments(t *testing.T) (*paneltest.Panel, *documentShelf) {
	t.Helper()

	return openDocumentsWith(t, paneltest.Params{})
}

func openDocumentsWith(t *testing.T, params paneltest.Params) (*paneltest.Panel, *documentShelf) {
	t.Helper()

	docs := newDocumentShelf()
	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(buildDocumentAuthors()...))
	docs.answer(harness)

	harness.MoveToRow("task-author")
	harness.Press("space", "J", "d")
	harness.Advance(time.Second)

	return harness, docs
}

// openEveryDocument opens the documents view on the switcher from an agent that wrote none.
func openEveryDocument(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, buildDocumentAuthors()...)
	newDocumentShelf().answer(harness)
	harness.MoveToRow("task-quiet")
	harness.Press("tab", "tab")
	harness.Advance(time.Second)

	return harness
}

// buildDocumentAuthors is a workflow whose orchestrator and one agent have written, beside an
// agent that has not.
func buildDocumentAuthors() []proto.Message {
	orchestrator := paneltest.Orchestrator()
	orchestrator.Documents = 2
	orchestrator.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))

	author := paneltest.Agent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
	quiet := paneltest.Agent("quiet", "quiet", "w1")

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		orchestrator,
		author,
		paneltest.TaskOf(author),
		quiet,
		paneltest.TaskOf(quiet),
		newDocumentType("t1", "RFC"),
		newDocumentType("t2", "report"),
		newDocumentType("t3", "explainer"),
		newDocumentType("t4", "notes"),
	}
}

// documentShelf is what the server holds of every agent's documents, answering the list and
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
			{
				Id:        "d1",
				AgentId:   "author",
				TaskId:    xptr.Ptr("task-author"),
				Name:      "Design the cache",
				Type:      "RFC",
				Version:   3,
				UpdatedAt: timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
			},
			{
				Id:        "d2",
				AgentId:   "author",
				TaskId:    xptr.Ptr("task-author"),
				Name:      "Rollout report",
				Type:      "report",
				Version:   5,
				UpdatedAt: timestamppb.New(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)),
			},
		},
		bodies: map[string]string{
			"d1": "# Design the cache\n\nKeep the cache warm.",
			"d2": "It went out on Tuesday.",
		},
		writes: map[string][]documentWrite{
			"d1": {
				{version: 1, body: "# Design the cache\n\nKeep the cache cold."},
				{version: 2, body: "# Design the cache\n\nKeep the cache cold.\n\nEvict nightly."},
				{
					version:  3,
					body:     "# Design the cache\n\nKeep the cache warm.",
					byPerson: true,
				},
			},
			"d2": {
				{version: 4, body: "It went out on Monday."},
				{version: 5, body: "It went out on Tuesday."},
			},
		},
	}
}

// rewrite changes a document's text and moves it to the top, as a write does.
func (d *documentShelf) rewrite(id, body string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.bodies[id] = body
	for _, document := range d.listed {
		if document.GetId() == id {
			document.UpdatedAt = dates.Stamp(at)
			document.Version++
			d.writes[id] = append(d.writes[id], documentWrite{version: document.GetVersion(), body: body})
		}
	}
}

// rename is the document given a new name at the time given.
func (d *documentShelf) rename(id, name string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, document := range d.listed {
		if document.GetId() == id {
			document.Name = name
			document.UpdatedAt = dates.Stamp(at)
		}
	}
}

// add is a document written beside the others.
func (d *documentShelf) add(document *agentv1.Document) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.listed = append(d.listed, document)
}

func (d *documentShelf) answer(harness *paneltest.Panel) {
	harness.Answer(agentv1connect.AgentServiceListDocumentsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		d.mu.Lock()
		defer d.mu.Unlock()

		asked, _ := req.Any().(*agentv1.ListDocumentsRequest)
		listed := make([]*agentv1.Document, 0, len(d.listed))
		for _, document := range d.listed {
			if asked.AgentId != nil && document.GetAgentId() != asked.GetAgentId() {
				continue
			}
			listed = append(listed, proto.CloneOf(document))
		}

		return connect.NewResponse(&agentv1.ListDocumentsResponse{Documents: listed}), nil
	})
	harness.Answer(agentv1connect.AgentServiceGetDocumentProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		d.mu.Lock()
		defer d.mu.Unlock()

		read, _ := req.Any().(*agentv1.GetDocumentRequest)
		for _, document := range d.listed {
			if document.GetId() != read.GetId() {
				continue
			}

			whole := proto.CloneOf(document)
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
	harness.Answer(agentv1connect.AgentServiceListDocumentVersionsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
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
				CreatedAt: timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
			})
		}

		return connect.NewResponse(&agentv1.ListDocumentVersionsResponse{Versions: versions}), nil
	})
}
