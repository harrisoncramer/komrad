package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	searchScreen  = "the search"
	queryCaret    = "▏"
	matchedTurn   = "the reconcile loop is what settles this"
	previewFiller = " and then a good deal more of it said in the same breath, going on for long " +
		"enough that no column the panel draws holds the whole of it on one row"
)

type searchServer struct {
	pages       map[string]*agentv1.SearchTranscriptsResponse
	transcripts map[string]string
	refusals    map[string]error
}

// TestSearchQualifiers covers where the search lists the qualifiers it takes.
func TestSearchQualifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		press []string
		want  bool
	}{
		{
			name:  "a query with the legend folded leaves them off",
			query: "ledger",
			press: []string{"enter"},
		},
		{
			name: "an empty query lists them",
			want: true,
		},
		{
			name:  "the expanded legend lists them under a query",
			query: "ledger",
			press: []string{"enter", "?"},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openSearch(t, openSearchParams{
				server: newSearchServer(newTurnHit("a2", "payouts", "ledger")),
				query:  tt.query,
				rows:   buildSearchable(),
			})
			harness.press(tt.press...)

			frame := harness.readFrame()
			for _, qualifier := range []string{"from:agent", "since:3d", "sort:new"} {
				if tt.want {
					assert.Contains(t, frame, qualifier)
				} else {
					assert.NotContains(t, frame, qualifier)
				}
			}
		})
	}
}

// TestSearchQuery covers the query line taking every key typed into it.
func TestSearchQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "backspace on an empty query is harmless",
			press: []string{"backspace"},
			want:  "",
		},
		{
			name:  "a multi-byte rune is dropped whole rather than half deleted",
			press: []string{"a", "é", "backspace"},
			want:  "a",
		},
		{
			name:  "backspace drops the last character",
			press: []string{"p", "o", "s", "t", "backspace"},
			want:  "pos",
		},
		{
			name:  "space is part of the query rather than a command",
			press: []string{"p", "o", "space", "g", "r"},
			want:  "po gr",
		},
		{
			name:  "the nav keys are typed while the line holds them",
			press: []string{"j", "k"},
			want:  "jk",
		},
		{
			name:  "the help key is typed rather than drawing the legend",
			press: []string{"l", "e", "d", "g", "e", "r", "?"},
			want:  "ledger?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openAgents(t)
			harness.press("tab", "/")

			harness.press(tt.press...)

			assert.Equal(t, "/ "+tt.want+queryCaret, readSearch(harness.readFrame()))
		})
	}
}

// TestSearchResults covers what the hits the server answers with draw as.
func TestSearchResults(t *testing.T) {
	t.Parallel()

	t.Run("a query that matched nothing draws no rows", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: &searchServer{},
			query:  "ledger",
			rows:   buildSearchable(),
		})

		assert.Empty(t, readListRows(harness.readFrame()))
	})

	t.Run("emptying the query clears the hits without asking again", func(t *testing.T) {
		t.Parallel()

		server := newSearchServer(newTurnHit("a2", "payouts", "ledger"))
		harness := openSearch(t, openSearchParams{
			server: server,
			query:  "led",
			rows:   buildSearchable(),
		})
		require.NotEmpty(t, readListRows(harness.readFrame()))

		harness.press("backspace", "backspace", "backspace")
		harness.fireTimers()

		assert.Empty(t, readListRows(harness.readFrame()))
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceSearchTranscriptsProcedure), 1)
	})

	t.Run("a refused query says why over the hits of the one before it", func(t *testing.T) {
		t.Parallel()

		server := newSearchServer(newTurnHit("a2", "payouts", "ledger"))
		server.refusals = map[string]error{
			"led since:x": connect.NewError(connect.CodeInvalidArgument, errors.New("since:x is not a day")),
		}
		harness := openSearch(t, openSearchParams{
			server: server,
			query:  "led",
			rows:   buildSearchable(),
		})
		hits := readListRows(harness.readFrame())
		require.NotEmpty(t, hits)

		typeText(harness, " since:x")
		harness.fireTimers()

		frame := harness.readFrame()
		assert.Contains(t, frame, "since:x is not a day")
		assert.Equal(t, hits, readListRows(frame))
	})

	t.Run("a search that failed says so rather than that nothing matched", func(t *testing.T) {
		t.Parallel()

		server := &searchServer{refusals: map[string]error{
			"ledger": connect.NewError(connect.CodeUnavailable, errors.New("the server is gone")),
		}}
		harness := openSearch(t, openSearchParams{
			server: server,
			query:  "ledger",
			rows:   buildSearchable(),
		})

		frame := harness.readFrame()
		assert.Contains(t, frame, "the server is gone")
		assert.NotContains(t, frame, "no matches")
	})

	t.Run("a search left before it settles is never sent", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSearchable()...)
		harness.press("tab", "/", "l", "e", "d", "esc", "shift+tab")

		harness.fireTimers()

		assert.Equal(t, agentsScreen, harness.readScreen())
		harness.assertSentTo(agentv1connect.AgentServiceSearchTranscriptsProcedure)
	})

	t.Run("an answer for a query moved past is dropped", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{})
		answerWorktreeReads(harness)
		newSearchServer(newTurnHit("a2", "payouts", "ledger")).serve(harness)
		harness.watch(buildSnapshot(buildSearchable()...))
		harness.hold(agentv1connect.AgentServiceSearchTranscriptsProcedure)
		searchFor(harness, "led")

		harness.press("g")
		harness.release(agentv1connect.AgentServiceSearchTranscriptsProcedure)

		assert.Empty(t, readListRows(harness.readFrame()))
	})

	t.Run("a preview for a query moved past is dropped", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{})
		content, offset, length := buildTranscript()
		server := &searchServer{
			pages:       map[string]*agentv1.SearchTranscriptsResponse{"": {Matches: []*agentv1.SearchHit{newMatchHit("a1", offset, length)}}},
			transcripts: map[string]string{"a1": content},
		}
		answerWorktreeReads(harness)
		server.serve(harness)
		harness.watch(buildSnapshot(buildSearchable()...))
		harness.hold(agentv1connect.AgentServiceGetTranscriptProcedure)
		searchFor(harness, "reconcile")
		require.NotEmpty(t, readListRows(harness.readFrame()))

		harness.press("r")
		harness.release(agentv1connect.AgentServiceGetTranscriptProcedure)

		assert.NotContains(t, readStageText(harness.readFrame()), matchedTurn)
	})

	t.Run("an agent is one row however many of its turns said the query", func(t *testing.T) {
		t.Parallel()

		server := newSearchServer(
			newTurnHit("a1", "dispatcher", "first"),
			newTurnHit("a2", "dispatcher", "second"),
			newTurnHit("a1", "dispatcher", "third"),
		)
		harness := openSearch(t, openSearchParams{
			server: server,
			query:  "ledger",
		})

		assert.Equal(t, []string{
			"dispatcher",
			"#0 first",
			"#0 third",
			"dispatcher",
			"#0 second",
		}, readSearchRows(harness.readFrame()))
	})

	t.Run("the agents whose own name says the query stand first, live before archived", func(t *testing.T) {
		t.Parallel()

		rows := append(buildSearchable(), newArchivedAgent("a3", "billing-old", "w1"))
		harness := openSearch(t, openSearchParams{
			server: newSearchServer(newTurnHit("a2", "payouts", "bill")),
			query:  "BILL",
			rows:   rows,
		})

		frame := harness.readFrame()
		assert.Equal(t, []string{
			"billing",
			"billing-old",
			"payouts",
			"#0 bill",
		}, readSearchRows(frame))
		assert.Contains(t, readHoveredRow(frame), "billing")
	})

	t.Run("an agent matched by its name and by a turn is one row carrying the turn", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: newSearchServer(newTurnHit("a1", "billing", "the billing ledger")),
			query:  "billing",
			rows:   buildSearchable(),
		})

		assert.Equal(t, []string{"billing", "#0 the billing ledger"}, readSearchRows(harness.readFrame()))
	})

	t.Run("more hits than the pane is tall are counted and the cursor still reaches the last", func(t *testing.T) {
		t.Parallel()

		var found []*agentv1.SearchHit
		for i := range 20 {
			found = append(found, newTurnHit(fmt.Sprintf("a%d", i), fmt.Sprintf("agent-%d", i), fmt.Sprintf("hit %d", i)))
		}
		harness := newPanelHarness(t, panelHarnessParams{height: 14})
		newSearchServer(found...).serve(harness)
		harness.watch(buildSnapshot())
		harness.press("tab", "/")
		typeText(harness, "agent")
		harness.fireTimers()

		assert.Contains(t, readSidebarProse(harness.readFrame()), "more")

		harness.press("enter", "G")

		assert.Contains(t, harness.readFrame(), "agent-19")
	})
}

// TestSearchKeys covers what the hits answer once enter has closed the query line.
func TestSearchKeys(t *testing.T) {
	t.Parallel()

	t.Run("the filter key over an empty result puts the keys back in the query line", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: &searchServer{},
			query:  "ledger",
		})

		harness.press("enter", "/")

		assert.Equal(t, "/ ledger"+queryCaret, readSearch(harness.readFrame()))
	})

	tests := []struct {
		name        string
		press       []string
		wantScreen  string
		wantHovered string
		wantTyping  bool
	}{
		{
			name:       "escape in the query line gives the keys up to the tabs",
			press:      []string{"esc", "shift+tab"},
			wantScreen: agentsScreen,
			wantTyping: false,
		},
		{
			name:       "escape over the hits opens the view switcher",
			press:      []string{"enter", "esc", "shift+tab"},
			wantScreen: agentsScreen,
		},
		{
			name:        "enter closes the line and leaves the query standing",
			press:       []string{"enter"},
			wantScreen:  searchScreen,
			wantHovered: "alpha",
		},
		{
			name:        "the nav keys run down the hits rather than typing",
			press:       []string{"enter", "j", "j"},
			wantScreen:  searchScreen,
			wantHovered: "bravo",
		},
		{
			name:        "the bottom key lands on the last turn",
			press:       []string{"enter", "G"},
			wantScreen:  searchScreen,
			wantHovered: "#0 charlie",
		},
		{
			name:        "the top key acts on its second press",
			press:       []string{"enter", "G", "g", "g"},
			wantScreen:  searchScreen,
			wantHovered: "alpha",
		},
		{
			name:        "the filter key puts the keys back in the query line and leaves the cursor",
			press:       []string{"enter", "j", "/"},
			wantScreen:  searchScreen,
			wantHovered: "#0 alpha",
			wantTyping:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := newSearchServer(
				newTurnHit("a", "alpha", "alpha"),
				newTurnHit("b", "bravo", "bravo"),
				newTurnHit("c", "charlie", "charlie"),
			)
			harness := openSearch(t, openSearchParams{
				server: server,
				query:  "ledger",
			})

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.wantScreen, harness.readScreen())
			if tt.wantScreen != searchScreen {
				return
			}
			assert.Equal(t, tt.wantHovered, readHoveredSearchRow(frame))
			assert.Equal(t, tt.wantTyping, strings.HasSuffix(readSearch(frame), queryCaret))
			assert.Contains(t, readSearch(frame), "ledger")
		})
	}

	t.Run("the step off a name hit lands on the conversation below it", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: newSearchServer(newTurnHit("a2", "payouts", "bill")),
			query:  "bill",
			rows:   buildSearchable(),
		})

		harness.press("enter", "j")

		assert.Equal(t, "payouts", readHoveredSearchRow(harness.readFrame()))
	})
}

// TestSearchLiveRow covers a live agent's hit drawing as the agents list draws it.
func TestSearchLiveRow(t *testing.T) {
	t.Parallel()

	live := newAgent("a1", "billing", "w1")
	live.State = agentv1.AgentState_AGENT_STATE_WAITING
	live.Alive = xptr.Ptr(true)
	live.Hidden = true
	live.HeldAt = "2026-09-20T12:00:00Z"
	live.ScheduleId = xptr.Ptr("s1")
	rows := []proto.Message{
		newLiveWorkflow("w1", "core"),
		live,
		newUnreadMail("a1"),
	}
	harness := openSearch(t, openSearchParams{
		server: newSearchServer(newTurnHit("a1", "billing", "first")),
		query:  "ledger",
		rows:   rows,
	})
	searched := findRow(readListRows(harness.readFrame()), "billing")

	harness.press("esc", "space", "h", "H")

	assert.Equal(t, strings.TrimSpace(findRow(readListRows(harness.readFrame()), "billing")), searched)
}

// TestSearchArchivedRow covers an archived agent's hit drawing as the archive draws it.
func TestSearchArchivedRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		state    agentv1.AgentState
		isHidden bool
	}{
		{
			name:  "an idle archived agent",
			state: agentv1.AgentState_AGENT_STATE_IDLE,
		},
		{
			name:     "an archived agent hidden before it was put away",
			state:    agentv1.AgentState_AGENT_STATE_IDLE,
			isHidden: true,
		},
		{
			name:  "an archived agent that never started",
			state: agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
		},
		{
			name:  "an archived agent a delete could not destroy",
			state: agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			archived := newArchivedAgent("a2", "payouts", "w1")
			archived.State = tt.state
			archived.Hidden = tt.isHidden
			hit := newTurnHit("a2", "payouts", "second")
			hit.Archived = true
			harness := openSearch(t, openSearchParams{
				server: newSearchServer(hit),
				query:  "ledger",
				rows:   []proto.Message{newLiveWorkflow("w1", "core"), archived},
			})
			searched := findRow(readListRows(harness.readFrame()), "payouts")

			harness.press("esc", "tab", "tab")

			assert.Equal(t, strings.TrimSpace(findRow(readListRows(harness.readFrame()), "payouts")), searched)
		})
	}
}

// TestSearchUnknownRow covers a hit on an agent the panel holds no readable row for.
func TestSearchUnknownRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows []proto.Message
		hit  *agentv1.SearchHit
	}{
		{
			name: "a hit named before the listing has landed",
			hit:  newTurnHit("gone", "billing", "first"),
		},
		{
			name: "an archived hit the panel holds no row for",
			hit: &agentv1.SearchHit{
				AgentId:  "a2",
				Name:     "billing",
				Archived: true,
				Snippet:  "first",
			},
		},
		{
			name: "an archived hit whose row carries no stamp",
			rows: []proto.Message{newLiveWorkflow("w1", "core"), newAgent("a2", "billing", "w1")},
			hit: &agentv1.SearchHit{
				AgentId:  "a2",
				Name:     "billing",
				Archived: true,
				Snippet:  "first",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openSearch(t, openSearchParams{
				server: newSearchServer(tt.hit),
				query:  "ledger",
				rows:   tt.rows,
			})

			assert.Equal(t, "billing", strings.TrimPrefix(findRow(readListRows(harness.readFrame()), "billing"), "  "))
			assert.Contains(t, readSidebar(harness.readFrame()), "› │ billing")
		})
	}
}

// TestSearchJump covers enter going to the row the hit's agent already has.
func TestSearchJump(t *testing.T) {
	t.Parallel()

	hiddenPayouts := newPanedAgent("a2", "payouts", "%2")
	hiddenPayouts.Hidden = true

	archived := newTurnHit("a3", "old-work", "two")
	archived.Archived = true

	tests := []struct {
		name        string
		rows        []proto.Message
		query       string
		hit         *agentv1.SearchHit
		wantScreen  string
		wantHovered string
		wantDrawn   string
		wantZoomed  bool
	}{
		{
			name:       "a hit the panel holds no row for leaves the cursor alone",
			rows:       buildSearchable(),
			query:      "ledger",
			hit:        newTurnHit("gone", "gone", "two"),
			wantScreen: agentsScreen,
		},
		{
			name:        "a live hit lands on its row in the tree and gives the panes the window back",
			rows:        buildSearchable(),
			query:       "ledger",
			hit:         newTurnHit("a2", "payouts", "two"),
			wantScreen:  agentsScreen,
			wantHovered: "payouts",
		},
		{
			name:        "a hidden hit draws the hidden agents so its row exists",
			rows:        []proto.Message{newLiveWorkflow("w1", "core"), newPanedAgent("a1", "billing", "%1"), hiddenPayouts},
			query:       "ledger",
			hit:         newTurnHit("a2", "payouts", "two"),
			wantScreen:  agentsScreen,
			wantHovered: "payouts",
		},
		{
			name:        "an archived hit lands in the archive, which keeps the window for its own record",
			rows:        append(buildSearchable(), newArchivedAgent("a3", "old-work", "w1")),
			query:       "ledger",
			hit:         archived,
			wantScreen:  archiveScreen,
			wantHovered: "old-work",
			wantDrawn:   "kept until deleted",
			wantZoomed:  true,
		},
		{
			name:        "a name hit goes to the agent the same way",
			rows:        buildSearchable(),
			query:       "payout",
			wantScreen:  agentsScreen,
			wantHovered: "payouts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := &searchServer{}
			if tt.hit != nil {
				server = newSearchServer(tt.hit)
			}
			harness := newPanelHarness(t, panelHarnessParams{})
			answerWorktreeReads(harness)
			server.serve(harness)
			harness.watch(buildSnapshot(tt.rows...))
			before := readHoveredRow(harness.readFrame())
			searchFor(harness, tt.query)

			harness.press("enter", "enter")

			frame := harness.readFrame()
			assert.Equal(t, tt.wantScreen, harness.readScreen())
			if tt.wantHovered == "" {
				assert.Equal(t, before, readHoveredRow(frame))
			}
			assert.Contains(t, readHoveredRow(frame), tt.wantHovered)
			assert.Contains(t, frame, tt.wantDrawn)
			assert.Equal(t, tt.wantZoomed, harness.panel.IsZoomed())
			harness.assertSentTo(agentv1connect.AgentServiceRestoreAgentsProcedure)
			harness.assertSentTo(agentv1connect.AgentServiceResumeAgentProcedure)
		})
	}
}

// TestSearchCursor covers the cursor keeping its row as the listings the name hits are read off change.
func TestSearchCursor(t *testing.T) {
	t.Parallel()

	t.Run("an agent whose name says the query joining the listing leaves the cursor and its preview", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: newSearchServer(newTurnHit("a2", "payouts", "bill")),
			query:  "bill",
			rows:   buildSearchable(),
		})
		harness.press("enter", "j")
		previews := len(harness.readSent(agentv1connect.AgentServiceGetTranscriptProcedure))

		harness.watch(buildPuts(newAgent("a4", "billing-two", "w1")))

		assert.Equal(t, "payouts", readHoveredSearchRow(harness.readFrame()))
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceGetTranscriptProcedure), previews)
	})

	t.Run("the hit under the cursor going lands it on what is left and fetches that preview", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: newSearchServer(newTurnHit("a2", "payouts", "bill")),
			query:  "bill",
			rows:   buildSearchable(),
		})

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "core"), newAgent("a2", "payouts", "w1")))

		assert.Equal(t, "payouts", readHoveredSearchRow(harness.readFrame()))
		sent := harness.readSent(agentv1connect.AgentServiceGetTranscriptProcedure)
		require.NotEmpty(t, sent)
		assert.Equal(t, "a2", sent[len(sent)-1].(*agentv1.GetTranscriptRequest).GetAgentId())
	})
}

// TestSearchTrail covers the trail under the switcher as a search is typed and its hits are
// moved through.
func TestSearchTrail(t *testing.T) {
	t.Parallel()

	harness := openSearch(t, openSearchParams{
		server: newSearchServer(newTurnHit("a2", "payouts", "bill")),
		query:  "bill",
		rows:   buildSearchable(),
	})
	assert.Equal(t, "query", readTrail(harness.readFrame()), "the query is still being typed")

	harness.press("enter")
	assert.Equal(t, "query › billing", readTrail(harness.readFrame()), "an agent whose name says the query")

	harness.press("G")
	frame := harness.readFrame()
	require.NotEqual(t, "payouts", readHoveredSearchRow(frame))
	assert.Equal(t, "query › payouts", readTrail(frame), "a hit names the agent it was said by")
}

// TestSearchPreview covers the transcript the search draws beside its hits.
func TestSearchPreview(t *testing.T) {
	t.Parallel()

	t.Run("a window too narrow for both halves draws the hits alone", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{width: 69})

		frame := harness.readFrame()
		assert.Contains(t, frame, "billing")
		assert.NotContains(t, frame, matchedTurn)
	})

	t.Run("a hit whose conversation has not been written says so", func(t *testing.T) {
		t.Parallel()

		harness := openSearch(t, openSearchParams{
			server: newSearchServer(newTurnHit("a1", "billing", "reconcile")),
			query:  "reconcile",
			rows:   buildSearchable(),
		})

		assert.Contains(t, readStageText(harness.readFrame()), "no transcript yet")
	})

	t.Run("the search takes the window and hands it back on the way out", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		require.True(t, harness.panel.IsZoomed())

		harness.press("esc", "shift+tab")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.False(t, harness.panel.IsZoomed())
	})

	t.Run("the preview opens on the match rather than the top of the transcript", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})

		preview := readStageText(harness.readFrame())
		assert.Contains(t, preview, matchedTurn)
		assert.Contains(t, preview, "line 199 of the conversation before")
		assert.NotContains(t, preview, "line 0 of the conversation before")
	})

	t.Run("the hits and the transcript stand side by side", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})

		frame := harness.readFrame()
		assert.Equal(t, searchScreen, harness.readScreen())
		assert.NotEmpty(t, strings.TrimSpace(readStage(frame)[0]))
	})

	t.Run("the transcript takes the left when the sidebar is asked to stand right", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{sidebarSide: config.SidebarSideRight})

		assert.True(t, strings.HasPrefix(strings.TrimLeft(splitLines(harness.readFrame())[0], " "), "line"))
	})

	t.Run("a resize squares off every row at the new width", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})

		harness.send(tea.WindowSizeMsg{
			Width:  120,
			Height: 40,
		})

		for _, row := range splitLines(harness.readDrawn()) {
			assert.Equal(t, 120, ansi.StringWidth(row))
		}
	})

	t.Run("the wheel over the transcript scrolls it and leaves the hits alone", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		before := harness.readFrame()

		harness.send(tea.MouseWheelMsg{
			X:      defaultHarnessWidth - 1,
			Y:      5,
			Button: tea.MouseWheelDown,
		})

		frame := harness.readFrame()
		assert.NotEqual(t, readStage(before)[0], readStage(frame)[0])
		assert.Equal(t, readHoveredSearchRow(before), readHoveredSearchRow(frame))
	})

	t.Run("the wheel over the hits moves through them and leaves the transcript alone", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		before := harness.readFrame()

		harness.send(tea.MouseWheelMsg{
			X:      1,
			Y:      5,
			Button: tea.MouseWheelDown,
		})

		frame := harness.readFrame()
		assert.NotEqual(t, readHoveredSearchRow(before), readHoveredSearchRow(frame))
		assert.Equal(t, readStage(before)[0], readStage(frame)[0])
	})

	t.Run("the leader twice hands the keys to the transcript and esc hands them back to the hits", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.press("enter")
		hovered := readHoveredSearchRow(harness.readFrame())

		harness.press("ctrl+space", "ctrl+space", "g")
		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "line 0 of the conversation before")
		assert.Equal(t, hovered, readHoveredSearchRow(frame))

		harness.fireTimers()
		assert.Equal(t, hovered, readHoveredSearchRow(harness.readFrame()), "the hits hold their arrow still")

		harness.press("esc", "j")
		assert.NotEqual(t, hovered, readHoveredSearchRow(harness.readFrame()))
	})

	t.Run("the transcript takes the keys with its cursor on the match", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.press("enter", "ctrl+space", "ctrl+space")

		raw := harness.readRawFrame()
		at := findPreviewCursor(raw)
		require.GreaterOrEqual(t, at, 0)
		assert.Contains(t, readStage(ansi.Strip(raw))[at], matchedTurn)
	})

	t.Run("the wheel over the focused transcript carries its cursor with the window", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.press("enter", "ctrl+space", "ctrl+space", "j")
		before := harness.readRawFrame()
		at := findPreviewCursor(before)
		require.GreaterOrEqual(t, at, 0)

		harness.send(tea.MouseWheelMsg{X: defaultHarnessWidth - 1, Y: 5, Button: tea.MouseWheelDown})

		after := harness.readRawFrame()
		assert.Equal(t, at, findPreviewCursor(after))
		assert.NotEqual(t, readStage(ansi.Strip(before))[at], readStage(ansi.Strip(after))[at])
	})

	t.Run("a width change keeps the focused transcript's cursor in the window", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.press("enter", "ctrl+space", "ctrl+space", "G")
		require.GreaterOrEqual(t, findPreviewCursor(harness.readRawFrame()), 0)

		harness.send(tea.WindowSizeMsg{Width: defaultHarnessWidth + 40, Height: defaultHarnessHeight})

		assert.GreaterOrEqual(t, findPreviewCursor(harness.readRawFrame()), 0)
	})

	t.Run("a height change keeps the reader's place", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.press("enter", "ctrl+n", "ctrl+n")
		scrolled := readStage(harness.readFrame())[0]

		harness.send(tea.WindowSizeMsg{
			Width:  defaultHarnessWidth,
			Height: 44,
		})

		assert.Equal(t, scrolled, readStage(harness.readFrame())[0])
	})

	t.Run("a window that had no rows to give draws the preview once it grows", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.send(tea.WindowSizeMsg{Width: defaultHarnessWidth})

		harness.send(tea.WindowSizeMsg{
			Width:  defaultHarnessWidth,
			Height: 30,
		})

		assert.Contains(t, readStageText(harness.readFrame()), matchedTurn)
	})

	t.Run("the keys that fill a pane stand down while the search holds the window", func(t *testing.T) {
		t.Parallel()

		harness := openPreview(t, panelHarnessParams{})
		harness.press("enter")
		respawns := harness.panel.CountRespawns()

		harness.press("l", "L", "T")

		assert.Equal(t, respawns, harness.panel.CountRespawns())
	})

	t.Run("stepping to another agent's hit opens its own transcript on its own match", func(t *testing.T) {
		t.Parallel()

		content, offset, length := buildTranscript()
		harness := openSearch(t, openSearchParams{
			server: &searchServer{
				pages: map[string]*agentv1.SearchTranscriptsResponse{"": {Matches: []*agentv1.SearchHit{
					newMatchHit("a1", 0, 0),
					newMatchHit("a2", offset, length),
				}}},
				transcripts: map[string]string{"a2": content},
			},
			query: "reconcile",
			rows:  buildSearchable(),
		})
		require.NotContains(t, readStageText(harness.readFrame()), matchedTurn)

		harness.press("enter", "j", "j", rest)

		assert.Contains(t, readStageText(harness.readFrame()), matchedTurn)
	})

	t.Run("a hit passed on the way to another reads no transcript for it", func(t *testing.T) {
		t.Parallel()

		content, offset, length := buildTranscript()
		harness := openSearch(t, openSearchParams{
			server: &searchServer{
				pages: map[string]*agentv1.SearchTranscriptsResponse{"": {Matches: []*agentv1.SearchHit{
					newMatchHit("a1", 0, 0),
					newMatchHit("a2", offset, length),
				}}},
				transcripts: map[string]string{"a2": content},
			},
			query: "reconcile",
			rows:  buildSearchable(),
		})

		harness.press("enter", "j", "j", "k", "k", rest)

		assert.False(t, slices.ContainsFunc(harness.readSent(agentv1connect.AgentServiceGetTranscriptProcedure), func(sent proto.Message) bool {
			return proto.Equal(&agentv1.GetTranscriptRequest{AgentId: "a2"}, sent)
		}))
	})

	t.Run("stepping between two turns of one conversation moves the transcript it holds", func(t *testing.T) {
		t.Parallel()

		content, offset, length := buildTranscript()
		early := newMatchHit("a1", int64(strings.Index(content, "line 3 of the conversation before")), length)
		early.Snippet = paint.MarkOpen + "conversation" + paint.MarkClose
		harness := openSearch(t, openSearchParams{
			server: &searchServer{
				pages: map[string]*agentv1.SearchTranscriptsResponse{"": {Matches: []*agentv1.SearchHit{
					newMatchHit("a1", offset, length),
					early,
				}}},
				transcripts: map[string]string{"a1": content},
			},
			query: "reconcile",
			rows:  buildSearchable(),
		})
		harness.press("enter", "j", rest)
		require.Contains(t, readStageText(harness.readFrame()), matchedTurn)

		harness.press("j", rest)
		assert.Contains(t, readStageText(harness.readFrame()), "line 3 of the conversation before")

		harness.press("k", rest)
		assert.Contains(t, readStageText(harness.readFrame()), matchedTurn)
		assert.Len(t, harness.readSent(agentv1connect.AgentServiceGetTranscriptProcedure), 1)
	})

	t.Run("the cursor reaching the end asks for the next page and keeps its turn as it lands", func(t *testing.T) {
		t.Parallel()

		content, offset, length := buildTranscript()
		early := int64(strings.Index(content, "line 3 of the conversation before"))
		second := newMatchHit("a2", offset, length)
		second.Seq = 9
		laterBilling := newMatchHit("a1", early, length)
		laterBilling.Seq = 3
		laterBilling.Snippet = paint.MarkOpen + "conversation" + paint.MarkClose
		laterPayouts := newMatchHit("a2", early, length)
		laterPayouts.Seq = 2
		laterPayouts.Snippet = laterBilling.Snippet
		harness := openSearch(t, openSearchParams{
			server: &searchServer{
				pages: map[string]*agentv1.SearchTranscriptsResponse{
					"": {
						Matches:       []*agentv1.SearchHit{newMatchHit("a1", offset, length), second},
						NextPageToken: xptr.Ptr("page-2"),
					},
					"page-2": {Matches: []*agentv1.SearchHit{laterBilling, laterPayouts}},
				},
				transcripts: map[string]string{
					"a1": content,
					"a2": content,
				},
			},
			query: "reconcile",
			rows:  buildSearchable(),
		})

		harness.press("enter", "G", rest)

		harness.assertSentTo(agentv1connect.AgentServiceSearchTranscriptsProcedure,
			&agentv1.SearchTranscriptsRequest{
				Query:      "reconcile",
				Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](10)},
			},
			&agentv1.SearchTranscriptsRequest{
				Query: "reconcile",
				Pagination: &paginationv1.Pagination{
					Limit:  xptr.Ptr[int32](10),
					Cursor: xptr.Ptr("page-2"),
				},
			},
		)
		frame := harness.readFrame()
		assert.Equal(t, []string{
			"billing",
			"agent #0 reconcile",
			"agent #3 conversation",
			"payouts",
			"agent #9 reconcile",
			"agent #2 conversation",
		}, readSearchRows(frame))
		assert.Equal(t, "agent #9 reconcile", readHoveredSearchRow(frame))
		assert.Contains(t, readStageText(frame), matchedTurn)

		harness.press("G")

		assert.Len(t, harness.readSent(agentv1connect.AgentServiceSearchTranscriptsProcedure), 2)
	})
}

type openSearchParams struct {
	harness panelHarnessParams
	server  *searchServer
	query   string
	rows    []proto.Message
}

func openSearch(t *testing.T, params openSearchParams) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params.harness)
	answerWorktreeReads(harness)
	params.server.serve(harness)
	harness.watch(buildSnapshot(params.rows...))
	searchFor(harness, params.query)

	return harness
}

func searchFor(harness *panelHarness, query string) {
	harness.press("tab", "/")
	typeText(harness, query)
	harness.fireTimers()
}

func openPreview(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	content, offset, length := buildTranscript()
	server := &searchServer{
		pages:       map[string]*agentv1.SearchTranscriptsResponse{"": {Matches: []*agentv1.SearchHit{newMatchHit("a1", offset, length)}}},
		transcripts: map[string]string{"a1": content},
	}

	return openSearch(t, openSearchParams{
		harness: params,
		server:  server,
		query:   "reconcile",
		rows:    buildSearchable(),
	})
}

func newSearchServer(hits ...*agentv1.SearchHit) *searchServer {
	return &searchServer{pages: map[string]*agentv1.SearchTranscriptsResponse{"": {Matches: hits}}}
}

func (s *searchServer) serve(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		search, _ := req.Any().(*agentv1.SearchTranscriptsRequest)
		if err := s.refusals[search.GetQuery()]; err != nil {
			return nil, err
		}
		page := s.pages[search.GetPagination().GetCursor()]
		if page == nil {
			page = &agentv1.SearchTranscriptsResponse{}
		}

		return connect.NewResponse(page), nil
	})
	harness.answer(agentv1connect.AgentServiceGetTranscriptProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*agentv1.GetTranscriptRequest)

		return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: s.transcripts[read.GetAgentId()]}), nil
	})
}

func buildSearchable() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "core"),
		newPanedAgent("a1", "billing", "%1"),
		newPanedAgent("a2", "payouts", "%2"),
	}
}

func newPanedAgent(id, name, pane string) *agentv1.Agent {
	agent := newAgent(id, name, "w1")
	agent.PaneId = pane
	agent.Alive = xptr.Ptr(true)

	return agent
}

func buildTranscript() (string, int64, int64) {
	var transcript strings.Builder
	for i := range 200 {
		fmt.Fprintf(&transcript, "line %d of the conversation before the match%s\n\n", i, previewFiller)
	}

	offset := int64(transcript.Len())
	transcript.WriteString(matchedTurn + "\n")

	for i := range 200 {
		fmt.Fprintf(&transcript, "\nline %d of the conversation after the match\n", i)
	}

	return transcript.String(), offset, int64(len(matchedTurn) + 1)
}

func newTurnHit(agentID, name, snippet string) *agentv1.SearchHit {
	return &agentv1.SearchHit{
		AgentId: agentID,
		Name:    name,
		Snippet: snippet,
	}
}

func newMatchHit(agentID string, offset, length int64) *agentv1.SearchHit {
	names := map[string]string{
		"a1": "billing",
		"a2": "payouts",
	}

	return &agentv1.SearchHit{
		AgentId:       agentID,
		Name:          names[agentID],
		Role:          "assistant",
		ContentOffset: offset,
		ContentLength: length,
		Snippet:       paint.MarkOpen + "reconcile" + paint.MarkClose,
	}
}

func newUnreadMail(agentID string) *agentv1.Mail {
	return &agentv1.Mail{
		Id:        "m1",
		ToAgentId: agentID,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

func readSearchRows(frame string) []string {
	return xslice.Map(readListRows(frame), strings.TrimSpace)
}

func readHoveredSearchRow(frame string) string {
	return strings.TrimSpace(readHoveredRow(frame))
}

func findRow(rows []string, name string) string {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row
		}
	}

	return ""
}

func findPreviewCursor(raw string) int {
	selected := paint.BackgroundSequence(paint.Panel.Background(paint.ShadeSelection))
	for i, line := range splitLines(raw) {
		stage := ansi.TruncateLeft(line, config.DefaultSidebarWidth, "")
		if strings.Contains(stage, selected) {
			return i
		}
	}

	return -1
}
