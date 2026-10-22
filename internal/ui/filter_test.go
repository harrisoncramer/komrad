package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	schedulev1 "komrad/proto/gen/schedule/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilterSearch covers the search over a list, which walks the cursor between matches and takes no row away.
func TestFilterSearch(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		press       []string
		wantHovered string
		wantSearch  string
	}{
		{
			name:        "escape while typing gives up and puts the cursor back",
			from:        "backend",
			press:       []string{"/", "d", "b", "esc"},
			wantHovered: "backend",
			wantSearch:  "/",
		},
		{
			name:        "escape over a standing search clears it and puts the cursor back",
			from:        "backend",
			press:       []string{"/", "d", "b", "enter", "esc"},
			wantHovered: "backend",
			wantSearch:  "/",
		},
		{
			name:        "a pattern nothing matches leaves the cursor where it is",
			from:        "├─ task-auth",
			press:       []string{"/", "z", "z", "z", "enter"},
			wantHovered: "├─ task-auth",
			wantSearch:  "/ zzz",
		},
		{
			name:        "opening the input again picks the search back up",
			from:        "backend",
			press:       []string{"/", "d", "b", "enter", "/", "x", "enter"},
			wantHovered: "└─ db",
			wantSearch:  "/ dbx",
		},
		{
			name:        "n does nothing before a search stands",
			from:        "├─ task-auth",
			press:       []string{"n"},
			wantHovered: "├─ task-auth",
			wantSearch:  "/",
		},
		{
			name:        "typing moves nothing",
			from:        "backend",
			press:       []string{"/", "d", "b"},
			wantHovered: "backend",
			wantSearch:  "/ db▏",
		},
		{
			name:        "enter puts the input away and walks onto the match",
			from:        "backend",
			press:       []string{"/", "d", "b", "enter"},
			wantHovered: "└─ db",
			wantSearch:  "/ db",
		},
		{
			name:        "the row the cursor is on counts as the nearest match",
			from:        "├─ task-auth",
			press:       []string{"/", "a", "u", "t", "h", "enter"},
			wantHovered: "├─ task-auth",
			wantSearch:  "/ auth",
		},
		{
			name:        "a header matches by its own name",
			from:        "├─ task-auth",
			press:       []string{"/", "^", "f", "r", "o", "n", "t", "enter"},
			wantHovered: "frontend",
			wantSearch:  "/ ^front",
		},
		{
			name:        "a key bound elsewhere is typed rather than acted on",
			from:        "backend",
			press:       []string{"/", "i"},
			wantHovered: "backend",
			wantSearch:  "/ i▏",
		},
		{
			name:        "backspace takes the last rune back",
			from:        "backend",
			press:       []string{"/", "p", "a", "backspace"},
			wantHovered: "backend",
			wantSearch:  "/ p▏",
		},
		{
			name:        "left moves the cursor back so what is typed lands before it",
			from:        "backend",
			press:       []string{"/", "a", "c", "left", "b"},
			wantHovered: "backend",
			wantSearch:  "/ ab[c]",
		},
		{
			name:        "ctrl+w takes the last word back",
			from:        "backend",
			press:       []string{"/", "f", "o", "o", "space", "b", "a", "r", "ctrl+w"},
			wantHovered: "backend",
			wantSearch:  "/ foo ▏",
		},
		{
			name:        "n walks forward from the row it is on",
			from:        "├─ task-auth",
			press:       []string{"/", "a", "enter", "n"},
			wantHovered: "│  └─ " + reviewerMark + "api",
			wantSearch:  "/ a",
		},
		{
			name:        "N walks back",
			from:        "├─ task-auth",
			press:       []string{"/", "a", "enter", "n", "N"},
			wantHovered: "├─ task-auth",
			wantSearch:  "/ a",
		},
		{
			name:        "a step back from the top wraps to the last match",
			from:        "backend",
			press:       []string{"/", "^", "(", "b", "a", "c", "k", "e", "n", "d", "|", "u", "i", ")", "$", "enter", "N"},
			wantHovered: "└─ ui",
			wantSearch:  "/ ^(backend|ui)$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFilterable(t, tt.from)

			harness.Press(tt.press...)

			frame := harness.TypedFrame()
			assert.Equal(t, filterableRows, paneltest.ListRows(frame), "a search never takes a row off the screen")
			assert.Equal(t, tt.wantHovered, paneltest.HoveredRow(frame))
			assert.Equal(t, tt.wantSearch, readSearch(frame))
		})
	}

	t.Run("a selection survives a search", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)

		harness.Press("v", "j", "j", "v", "/", "d", "b", "enter", "space", "a")

		assert.Contains(t, harness.Frame(), "2 agents › archive")
	})

	t.Run("the search stays on the screen it was typed on", func(t *testing.T) {
		harness := openFilterable(t, "backend")

		harness.Press("/", "d", "b", "enter", "tab", "tab")

		assert.Equal(t, "/", readSearch(harness.Frame()))
	})

	t.Run("a click on a row while typing keeps the search and hands the keys to the list", func(t *testing.T) {
		harness := openFilterable(t, "backend")
		harness.Press("/", "d", "b")

		harness.Send(harness.ClickOn("frontend"))
		harness.Press("j")

		assert.Equal(t, "/ db", readSearch(harness.Frame()))
	})

	t.Run("a click on the search line while typing in it types nothing", func(t *testing.T) {
		harness := openFilterable(t, "backend")
		harness.Press("/", "d")

		harness.Send(harness.ClickOnLineStart("/"))
		harness.Press("b", "enter")

		assert.Equal(t, "/ db", readSearch(harness.Frame()))
	})
}

// TestFilterNarrow covers the filter, which takes off the list every row its pattern keeps no reason to show.
func TestFilterNarrow(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		press       []string
		wantRows    []string
		wantHovered string
		wantSearch  string
	}{
		{
			name:        "escape while typing gives every row back",
			from:        "frontend",
			press:       []string{"/", "tab", "d", "b", "esc"},
			wantRows:    filterableRows,
			wantHovered: "frontend",
			wantSearch:  "/",
		},
		{
			name:        "escape over a standing filter gives every row back",
			from:        "frontend",
			press:       []string{"/", "tab", "d", "b", "enter", "esc"},
			wantRows:    filterableRows,
			wantHovered: "frontend",
			wantSearch:  "/",
		},
		{
			name:        "a pattern that will not compile takes nothing off the list",
			from:        "backend",
			press:       []string{"/", "tab", "a", "u", "t", "h", "[", "enter"},
			wantRows:    filterableRows,
			wantHovered: "backend",
			wantSearch:  "▽ auth[",
		},
		{
			name:        "tab while typing flips to a filter that narrows nothing until enter",
			from:        "backend",
			press:       []string{"/", "d", "b", "tab"},
			wantRows:    filterableRows,
			wantHovered: "backend",
			wantSearch:  "▽ db▏",
		},
		{
			name:        "enter narrows to the match and the header above it",
			from:        "backend",
			press:       []string{"/", "d", "b", "tab", "enter"},
			wantRows:    []string{"backend", "└─ db"},
			wantHovered: "backend",
			wantSearch:  "▽ db",
		},
		{
			name:        "a second tab puts the search back",
			from:        "backend",
			press:       []string{"/", "d", "b", "tab", "tab", "enter"},
			wantRows:    filterableRows,
			wantHovered: "└─ db",
			wantSearch:  "/ db",
		},
		{
			name:        "a matching task keeps its reviewers",
			from:        "backend",
			press:       slices.Concat([]string{"/", "tab"}, spellKeys("^task-auth$"), []string{"enter"}),
			wantRows:    []string{"backend", "└─ task-auth", "   └─ " + reviewerMark + "api"},
			wantHovered: "backend",
			wantSearch:  "▽ ^task-auth$",
		},
		{
			name:        "a qualifier the writer holds for keeps its task",
			from:        "backend",
			press:       slices.Concat([]string{"/", "tab"}, spellKeys("agent:auth is:writer"), []string{"enter"}),
			wantRows:    []string{"backend", "└─ task-auth", "   └─ " + reviewerMark + "api"},
			wantHovered: "backend",
			wantSearch:  "▽ agent:auth is:writer",
		},
		{
			name:        "a matching reviewer keeps its task and the headers above that",
			from:        "backend",
			press:       []string{"/", "tab", "^", "a", "p", "i", "$", "enter"},
			wantRows:    []string{"backend", "└─ task-auth", "   └─ " + reviewerMark + "api"},
			wantHovered: "backend",
			wantSearch:  "▽ ^api$",
		},
		{
			name:        "a matching header keeps the whole group under it",
			from:        "backend",
			press:       []string{"/", "tab", "^", "b", "a", "c", "k", "e", "n", "d", "$", "enter"},
			wantRows:    []string{"backend", "├─ task-auth", "│  └─ " + reviewerMark + "api", "└─ db"},
			wantHovered: "backend",
			wantSearch:  "▽ ^backend$",
		},
		{
			name:        "a row it takes away hands the cursor to the nearest match forward",
			from:        "└─ db",
			press:       []string{"/", "u", "tab", "enter"},
			wantRows:    []string{"backend", "└─ task-auth", "   └─ " + reviewerMark + "api", "frontend", "└─ ui"},
			wantHovered: "└─ ui",
			wantSearch:  "▽ u",
		},
		{
			name:        "a row it keeps under a matched header holds the cursor",
			from:        "├─ task-auth",
			press:       []string{"/", "b", "a", "c", "k", "e", "n", "d", "|", "a", "p", "i", "tab", "enter"},
			wantRows:    []string{"backend", "├─ task-auth", "│  └─ " + reviewerMark + "api", "└─ db"},
			wantHovered: "├─ task-auth",
			wantSearch:  "▽ backend|api",
		},
		{
			name:        "the last row taken away hands the cursor back up to a match",
			from:        "└─ ui",
			press:       []string{"/", "^", "a", "p", "tab", "enter"},
			wantRows:    []string{"backend", "└─ task-auth", "   └─ " + reviewerMark + "api"},
			wantHovered: "   └─ " + reviewerMark + "api",
			wantSearch:  "▽ ^ap",
		},
		{
			name:        "a qualifier narrows to the rows it holds for",
			from:        "backend",
			press:       slices.Concat([]string{"/", "tab"}, spellKeys("workflow:front"), []string{"enter"}),
			wantRows:    []string{"frontend", "└─ ui"},
			wantHovered: "frontend",
			wantSearch:  "▽ workflow:front",
		},
		{
			name:        "a qualifier and a pattern must both hold",
			from:        "backend",
			press:       slices.Concat([]string{"/", "tab"}, spellKeys("workflow:back db"), []string{"enter"}),
			wantRows:    []string{"backend", "└─ db"},
			wantHovered: "backend",
			wantSearch:  "▽ workflow:back db",
		},
		{
			name:        "an unknown qualifier is a problem on the line and takes nothing off",
			from:        "backend",
			press:       slices.Concat([]string{"/", "tab"}, spellKeys("colour:red"), []string{"enter"}),
			wantRows:    filterableRows,
			wantHovered: "backend",
			wantSearch:  "▽ colour:red  colour: is not a qualifier her",
		},
		{
			name:        "a role the agents never take is a problem on the line",
			from:        "backend",
			press:       slices.Concat([]string{"/", "tab"}, spellKeys("is:boss db"), []string{"enter"}),
			wantRows:    filterableRows,
			wantHovered: "backend",
			wantSearch:  "▽ is:boss db  is:boss is not one of writer,",
		},
		{
			name:        "opening the input again picks the filter back up",
			from:        "backend",
			press:       []string{"/", "tab", "d", "b", "enter", "/", "enter"},
			wantRows:    []string{"backend", "└─ db"},
			wantHovered: "backend",
			wantSearch:  "▽ db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFilterable(t, tt.from)

			harness.Press(tt.press...)

			frame := harness.Frame()
			assert.Equal(t, tt.wantRows, paneltest.ListRows(frame))
			assert.Equal(t, tt.wantHovered, paneltest.HoveredRow(frame))
			assert.Equal(t, tt.wantSearch, readSearch(frame))
		})
	}
}

// TestFilterSays covers the says: qualifier answered by a transcript search on the server.
func TestFilterSays(t *testing.T) {
	t.Run("a says: search the server refuses stays a problem while its text stands", func(t *testing.T) {
		harness := openFilterable(t, "backend")
		harness.Refuse(agentv1connect.AgentServiceSearchTranscriptsProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no search")))

		harness.Press(slices.Concat([]string{"/", "tab"}, spellKeys("says:x"))...)
		harness.Advance(time.Second)
		harness.Press(slices.Concat(spellKeys(" d"), []string{"enter"})...)

		frame := harness.Frame()
		assert.Equal(t, filterableRows, paneltest.ListRows(frame))
		assert.Equal(t, "backend", paneltest.HoveredRow(frame))
		assert.Equal(t, "▽ says:x d  no search", readSearch(frame))
	})

	t.Run("a says: answer opens the hovered agent's conversation beside the list on its first match", func(t *testing.T) {
		tests := []struct {
			name        string
			open        func(*testing.T) *paneltest.Panel
			found       string
			wantHovered string
		}{
			{
				name:        "on the live agents",
				open:        func(t *testing.T) *paneltest.Panel { return openFilterable(t, "backend") },
				found:       "db",
				wantHovered: "└─ db",
			},
			{
				name: "on the archived agents",
				open: func(t *testing.T) *paneltest.Panel {
					return openArchivedAgents(t,
						newLiveWorkflow("w1", "backend"),
						newArchivedAgent("a1", "api-refactor", "w1"),
						newArchivedAgent("a2", "db-migrate", "w1"),
					)
				},
				found:       "a2",
				wantHovered: "├─ db-migrate",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := tt.open(t)
				turn := "the xylophone broke"
				content := "opening words\n\n" + strings.Repeat("filler\n\n", 80) + turn + "\n\n" + strings.Repeat("after\n\n", 80)
				harness.Answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{
						{
							AgentId:       tt.found,
							Snippet:       "the \x02xylophone\x03 broke",
							ContentOffset: int64(strings.Index(content, turn)),
							ContentLength: int64(len(turn)),
						},
					}}), nil
				})
				harness.Answer(agentv1connect.AgentServiceGetTranscriptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: content}), nil
				})

				harness.Press(slices.Concat([]string{"/"}, spellKeys("says:xylophone"), []string{"enter"})...)
				harness.Advance(time.Second)

				frame := harness.Frame()
				require.Equal(t, tt.wantHovered, paneltest.HoveredRow(frame))
				stage := paneltest.JoinProse(paneltest.Stage(frame))
				assert.Contains(t, stage, turn)
				assert.NotContains(t, stage, "opening words")
			})
		}
	})

	t.Run("a says: text edited while the same agent stays hovered moves the conversation to the new first match", func(t *testing.T) {
		harness := openFilterable(t, "└─ db")
		first, second := "the xylophone broke", "the zither snapped"
		content := "opening words\n\n" + first + "\n\n" + strings.Repeat("filler\n\n", 80) + second + "\n\n" + strings.Repeat("after\n\n", 80)
		harness.Answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
			turn, word := first, "xylophone"
			if strings.Contains(req.Any().(*agentv1.SearchTranscriptsRequest).GetQuery(), "zither") {
				turn, word = second, "zither"
			}

			return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{
				{
					AgentId:       "db",
					Snippet:       "\x02" + word + "\x03",
					ContentOffset: int64(strings.Index(content, turn)),
					ContentLength: int64(len(turn)),
				},
			}}), nil
		})
		harness.Answer(agentv1connect.AgentServiceGetTranscriptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: content}), nil
		})

		harness.Press(slices.Concat([]string{"/"}, spellKeys("says:xylophone"))...)
		harness.Advance(time.Second)
		require.Contains(t, paneltest.JoinProse(paneltest.Stage(harness.Frame())), first)

		harness.Press(slices.Concat(slices.Repeat([]string{"backspace"}, len("xylophone")), spellKeys("zither"), []string{"enter"})...)
		harness.Advance(time.Second)

		frame := harness.Frame()
		require.Equal(t, "└─ db", paneltest.HoveredRow(frame))
		stage := paneltest.JoinProse(paneltest.Stage(frame))
		assert.Contains(t, stage, second)
		assert.NotContains(t, stage, first)
	})

	t.Run("a says: query edited away while typed hands the window back to a live pane", func(t *testing.T) {
		tests := []struct {
			name  string
			press []string
		}{
			{name: "backspaced away", press: slices.Repeat([]string{"backspace"}, len("says:xylophone"))},
			{name: "escaped", press: []string{"esc"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				live := newAgentAt("db", "w1", 3)
				live.PaneId = "%9"
				live.Alive = xptr.Ptr(true)
				harness := openAgents(t, newLiveWorkflow("w1", "backend"), live)
				harness.Answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{{AgentId: "db", Snippet: "\x02xylophone\x03"}}}), nil
				})
				harness.Press("j")
				harness.Press(slices.Concat([]string{"/"}, spellKeys("says:xylophone"))...)
				harness.Advance(time.Second)
				require.Equal(t, "└─ db", paneltest.HoveredRow(harness.Frame()))
				require.True(t, harness.Tmux().IsZoomed(), "the turns it said hold the window")

				harness.Press(tt.press...)

				assert.False(t, harness.Tmux().IsZoomed(), "the live pane has its window back")
			})
		}
	})

	tests := []struct {
		name        string
		press       []string
		wantHovered string
	}{
		{
			name:        "a says: answer walks a cursor left where the search put it onto the match",
			press:       slices.Concat([]string{"/"}, spellKeys("says:x"), []string{"enter"}),
			wantHovered: "└─ db",
		},
		{
			name:        "a says: answer leaves a cursor moved while it was asked",
			press:       slices.Concat([]string{"/"}, spellKeys("says:x"), []string{"enter", "j"}),
			wantHovered: "├─ task-auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFilterable(t, "backend")
			harness.Answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{{AgentId: "db"}}}), nil
			})

			harness.Press(tt.press...)
			harness.Advance(time.Second)

			assert.Equal(t, tt.wantHovered, paneltest.HoveredRow(harness.Frame()))
		})
	}
}

// TestFilterScreens covers every list screen answering the same search and filter keys.
func TestFilterScreens(t *testing.T) {
	t.Run("the archive walks its matches and clears back to where it opened", func(t *testing.T) {
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgentAt("api-refactor", "2026-07-30T09:00:00Z"),
			newArchivedAgentAt("auth-fix", "2026-07-30T08:00:00Z"),
		)
		require.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))

		harness.Press("/", "^", "a", "enter")
		assert.Equal(t, "├─ api-refactor", paneltest.HoveredRow(harness.Frame()))

		harness.Press("n")
		assert.Equal(t, "└─ auth-fix", paneltest.HoveredRow(harness.Frame()))

		harness.Press("N")
		assert.Equal(t, "├─ api-refactor", paneltest.HoveredRow(harness.Frame()))

		harness.Press("esc")
		assert.Equal(t, "backend", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("the schedules walk their matches and narrow to them", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{})
		harness.Watch(paneltest.Snapshot(
			newLiveWorkflow("w1", "backend"),
			newNamedSchedule("s1", "nightly"),
			newNamedSchedule("s2", "morning"),
			newNamedSchedule("s3", "weekly"),
		))
		readSchedules := func() ([]string, string) {
			var names []string
			var hovered string
			for _, line := range paneltest.Sidebar(harness.Frame()) {
				if _, row, isRow := strings.Cut(line, "◇ "); isRow {
					names = append(names, strings.Fields(row)[0])
					if strings.HasPrefix(line, "›") {
						hovered = names[len(names)-1]
					}
				}
			}

			return names, hovered
		}
		harness.Press("tab")

		harness.Press("/", "l", "y", "enter")
		names, hovered := readSchedules()
		assert.Equal(t, []string{"weekly", "morning", "nightly"}, names, "a search leaves the list whole")
		assert.Equal(t, "weekly", hovered)

		harness.Press("n")
		_, hovered = readSchedules()
		assert.Equal(t, "nightly", hovered, "n walks to the next match")

		harness.Press("N")
		_, hovered = readSchedules()
		assert.Equal(t, "weekly", hovered, "and N walks back")

		harness.Press("esc", "/", "tab", "l", "y", "enter")
		names, _ = readSchedules()
		assert.Equal(t, []string{"weekly", "nightly"}, names)
	})

	t.Run("the documents walk their matches and narrow to them", func(t *testing.T) {
		harness, _ := openDocuments(t)

		harness.Press("esc", "/", "r", "o", "l", "l", "enter")
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "Rollout report")
		harness.Advance(time.Second)
		assert.Contains(t, readStageText(harness.Frame()), "It went out on Tuesday.")

		harness.Press("esc", "/", "tab", "r", "o", "l", "l", "enter")
		harness.Advance(time.Second)
		frame := harness.Frame()
		assert.NotContains(t, paneltest.SidebarProse(frame), "Design the cache")
		assert.Contains(t, readStageText(frame), "It went out on Tuesday.")
	})

	tests := []struct {
		name  string
		press []string
		idle  string
	}{
		{
			name: "the agents say a filter left nothing",
		},
		{
			name:  "the archived agents say a filter left nothing rather than that nothing is archived",
			press: []string{"F", "tab", "esc"},
			idle:  "nothing archived",
		},
		{
			name:  "the schedules say a filter left nothing rather than that there are none",
			press: []string{"tab"},
			idle:  "no schedules yet",
		},
		{
			name:  "the archived schedules say a filter left nothing rather than that none are archived",
			press: []string{"tab", "F", "tab", "esc"},
			idle:  "no archived schedules",
		},
		{
			name:  "the worktrees say a filter left nothing rather than that there are none",
			press: []string{"shift+tab"},
			idle:  "no worktrees",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{})
			answerWorktreeReads(harness)
			harness.Watch(paneltest.Snapshot(
				newLiveWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "old", "w1"),
				newNamedSchedule("s1", "nightly"),
				newArchivedSchedule("monthly-gone"),
				&worktreev1.WatchedWorktree{
					Dir:      "/trees/auth",
					RepoDir:  "/home/h/komrad",
					Branch:   "agent/auth",
					Readable: true,
				},
			))

			harness.Press(append(tt.press, "/", "tab", "z", "z", "z", "enter")...)

			sidebar := paneltest.SidebarProse(harness.Frame())
			assert.Contains(t, sidebar, "nothing matches")
			if tt.idle != "" {
				assert.NotContains(t, sidebar, tt.idle)
			}
		})
	}
}

// TestFilterWorktreeHeader covers a repository's header counting every worktree under it while a filter narrows them.
func TestFilterWorktreeHeader(t *testing.T) {
	harness := paneltest.Start(t, paneltest.Params{})
	harness.Watch(paneltest.Snapshot(
		newLiveWorkflow("w1", "backend"),
		&worktreev1.WatchedWorktree{
			Dir:        "/trees/worktree-view",
			RepoDir:    "/home/h/komrad",
			Branch:     "agent/worktree-view",
			Readable:   true,
			SizeBytes:  xptr.Ptr[int64](1 << 20),
			ShareBytes: xptr.Ptr[int64](1 << 20),
			MeasuredAt: xptr.Ptr(archivedAt),
		},
		&worktreev1.WatchedWorktree{
			Dir:        "/trees/other",
			RepoDir:    "/home/h/komrad",
			Branch:     "agent/other",
			Readable:   true,
			SizeBytes:  xptr.Ptr[int64](1 << 20),
			ShareBytes: xptr.Ptr[int64](1 << 20),
			MeasuredAt: xptr.Ptr(archivedAt),
		},
	))
	harness.Press("shift+tab", "esc", "f", "shift+tab", "j", "esc", "k")
	header := paneltest.BelowViews(harness.Frame())[5]
	require.Contains(t, header, "2.0M")

	harness.Press(slices.Concat([]string{"/", "tab"}, spellKeys("worktree-view"), []string{"enter", "k"})...)

	frame := harness.Frame()
	assert.NotContains(t, frame, "agent/other", "the filter took the other worktree away")
	assert.Equal(t, header, paneltest.BelowViews(frame)[5], "and the header's size still reads the whole listing")
	assert.Contains(t, frame, "2 worktrees", "as does its count")
}

var filterableRows = []string{"backend", "├─ task-auth", "│  └─ " + reviewerMark + "api", "└─ db", "frontend", "└─ ui"}

func openFilterable(t *testing.T, from string) *paneltest.Panel {
	t.Helper()

	auth := newAgentAt("auth", "w1", 5)
	harness := openAgents(t,
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		auth,
		newTaskOf(auth),
		newReviewer("api", "api", "auth"),
		newAgentAt("db", "w1", 3),
		newAgentAt("ui", "w2", 1),
	)

	harness.Press("g", "g")
	for range filterableRows {
		if paneltest.HoveredRow(harness.Frame()) == from {
			return harness
		}
		harness.Press("j")
	}
	require.Equal(t, from, paneltest.HoveredRow(harness.Frame()))

	return harness
}

func newNamedSchedule(id, name string) *schedulev1.Schedule {
	return &schedulev1.Schedule{
		Id:         id,
		Name:       name,
		WorkflowId: "w1",
		Cron:       "0 9 * * *",
	}
}
