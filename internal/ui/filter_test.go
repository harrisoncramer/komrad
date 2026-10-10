package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"komrad/internal/ui"
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
			from:        "├─ auth",
			press:       []string{"/", "z", "z", "z", "enter"},
			wantHovered: "├─ auth",
			wantSearch:  "/ zzz",
		},
		{
			name:        "n does nothing before a search stands",
			from:        "├─ auth",
			press:       []string{"n"},
			wantHovered: "├─ auth",
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
			from:        "├─ auth",
			press:       []string{"/", "a", "u", "t", "h", "enter"},
			wantHovered: "├─ auth",
			wantSearch:  "/ auth",
		},
		{
			name:        "a header matches by its own name",
			from:        "├─ auth",
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
			from:        "├─ auth",
			press:       []string{"/", "a", "enter", "n"},
			wantHovered: "│  └─ api",
			wantSearch:  "/ a",
		},
		{
			name:        "N walks back",
			from:        "├─ auth",
			press:       []string{"/", "a", "enter", "n", "N"},
			wantHovered: "├─ auth",
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

			harness.press(tt.press...)

			frame := harness.readTypedFrame()
			assert.Equal(t, filterableRows, readListRows(frame), "a search never takes a row off the screen")
			assert.Equal(t, tt.wantHovered, readHoveredRow(frame))
			assert.Equal(t, tt.wantSearch, readSearch(frame))
		})
	}

	t.Run("a selection survives a search", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)

		harness.press("v", "j", "j", "v", "/", "d", "b", "enter", "space", "a")

		assert.Contains(t, harness.readFrame(), "2 tasks › archive")
	})

	t.Run("the search stays on the screen it was typed on", func(t *testing.T) {
		harness := openFilterable(t, "backend")

		harness.press("/", "d", "b", "enter", "tab", "tab")

		assert.Equal(t, "/", readSearch(harness.readFrame()))
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
			name:        "a matching parent keeps its children",
			from:        "backend",
			press:       []string{"/", "tab", "^", "a", "u", "t", "h", "$", "enter"},
			wantRows:    []string{"backend", "└─ auth", "   └─ api"},
			wantHovered: "backend",
			wantSearch:  "▽ ^auth$",
		},
		{
			name:        "a matching child keeps its parent and the header above that",
			from:        "backend",
			press:       []string{"/", "tab", "^", "a", "p", "i", "$", "enter"},
			wantRows:    []string{"backend", "└─ auth", "   └─ api"},
			wantHovered: "backend",
			wantSearch:  "▽ ^api$",
		},
		{
			name:        "a matching header keeps the whole group under it",
			from:        "backend",
			press:       []string{"/", "tab", "^", "b", "a", "c", "k", "e", "n", "d", "$", "enter"},
			wantRows:    []string{"backend", "├─ auth", "│  └─ api", "└─ db"},
			wantHovered: "backend",
			wantSearch:  "▽ ^backend$",
		},
		{
			name:        "a row it takes away hands the cursor to the nearest match forward",
			from:        "└─ db",
			press:       []string{"/", "u", "tab", "enter"},
			wantRows:    []string{"backend", "└─ auth", "   └─ api", "frontend", "└─ ui"},
			wantHovered: "└─ ui",
			wantSearch:  "▽ u",
		},
		{
			name:        "a row it keeps under a matched header holds the cursor",
			from:        "├─ auth",
			press:       []string{"/", "b", "a", "c", "k", "e", "n", "d", "|", "a", "p", "i", "tab", "enter"},
			wantRows:    []string{"backend", "├─ auth", "│  └─ api", "└─ db"},
			wantHovered: "├─ auth",
			wantSearch:  "▽ backend|api",
		},
		{
			name:        "the last row taken away hands the cursor back up to a match",
			from:        "└─ ui",
			press:       []string{"/", "^", "a", "u", "tab", "enter"},
			wantRows:    []string{"backend", "└─ auth", "   └─ api"},
			wantHovered: "└─ auth",
			wantSearch:  "▽ ^au",
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
			wantSearch:  "▽ is:boss db  is:boss is not one of implemen",
		},
		{
			name:        "opening the input again starts a search",
			from:        "backend",
			press:       []string{"/", "tab", "d", "b", "enter", "/"},
			wantRows:    filterableRows,
			wantHovered: "backend",
			wantSearch:  "/ ▏",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFilterable(t, tt.from)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.wantRows, readListRows(frame))
			assert.Equal(t, tt.wantHovered, readHoveredRow(frame))
			assert.Equal(t, tt.wantSearch, readSearch(frame))
		})
	}
}

// TestFilterSays covers the says: qualifier answered by a transcript search on the server.
func TestFilterSays(t *testing.T) {
	t.Run("a says: search the server refuses stays a problem while its text stands", func(t *testing.T) {
		harness := openFilterable(t, "backend")
		harness.refuse(agentv1connect.AgentServiceSearchTranscriptsProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no search")))

		harness.press(slices.Concat([]string{"/", "tab"}, spellKeys("says:x"))...)
		harness.fireMatching(ui.IsSaysSettled)
		harness.press(slices.Concat(spellKeys(" d"), []string{"enter"})...)

		frame := harness.readFrame()
		assert.Equal(t, filterableRows, readListRows(frame))
		assert.Equal(t, "backend", readHoveredRow(frame))
		assert.Equal(t, "▽ says:x d  no search", readSearch(frame))
	})

	t.Run("a says: answer opens the hovered agent's conversation beside the list on its first match", func(t *testing.T) {
		tests := []struct {
			name        string
			open        func(*testing.T) *panelHarness
			found       string
			wantHovered string
		}{
			{
				name:        "on the live agents",
				open:        func(t *testing.T) *panelHarness { return openFilterable(t, "backend") },
				found:       "db",
				wantHovered: "└─ db",
			},
			{
				name: "on the archived agents",
				open: func(t *testing.T) *panelHarness {
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
				harness.answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{
						{
							AgentId:       tt.found,
							Snippet:       "the \x02xylophone\x03 broke",
							ContentOffset: int64(strings.Index(content, turn)),
							ContentLength: int64(len(turn)),
						},
					}}), nil
				})
				harness.answer(agentv1connect.AgentServiceGetTranscriptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: content}), nil
				})

				harness.press(slices.Concat([]string{"/"}, spellKeys("says:xylophone"), []string{"enter"})...)
				harness.fireMatching(ui.IsSaysSettled)

				frame := harness.readFrame()
				require.Equal(t, tt.wantHovered, readHoveredRow(frame))
				stage := joinProse(readStage(frame))
				assert.Contains(t, stage, turn)
				assert.NotContains(t, stage, "opening words")
			})
		}
	})

	t.Run("a says: text edited while the same agent stays hovered moves the conversation to the new first match", func(t *testing.T) {
		harness := openFilterable(t, "└─ db")
		first, second := "the xylophone broke", "the zither snapped"
		content := "opening words\n\n" + first + "\n\n" + strings.Repeat("filler\n\n", 80) + second + "\n\n" + strings.Repeat("after\n\n", 80)
		harness.answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
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
		harness.answer(agentv1connect.AgentServiceGetTranscriptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.GetTranscriptResponse{Content: content}), nil
		})

		harness.press(slices.Concat([]string{"/"}, spellKeys("says:xylophone"))...)
		harness.fireMatching(ui.IsSaysSettled)
		require.Contains(t, joinProse(readStage(harness.readFrame())), first)

		harness.press(slices.Concat(slices.Repeat([]string{"backspace"}, len("xylophone")), spellKeys("zither"), []string{"enter"})...)
		harness.fireMatching(ui.IsSaysSettled)

		frame := harness.readFrame()
		require.Equal(t, "└─ db", readHoveredRow(frame))
		stage := joinProse(readStage(frame))
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
				harness.answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{{AgentId: "db", Snippet: "\x02xylophone\x03"}}}), nil
				})
				harness.press("j")
				harness.press(slices.Concat([]string{"/"}, spellKeys("says:xylophone"))...)
				harness.fireMatching(ui.IsSaysSettled)
				require.Equal(t, "└─ db", readHoveredRow(harness.readFrame()))
				require.True(t, harness.panel.IsZoomed(), "the turns it said hold the window")

				harness.press(tt.press...)

				assert.False(t, harness.panel.IsZoomed(), "the live pane has its window back")
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
			wantHovered: "├─ auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFilterable(t, "backend")
			harness.answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{{AgentId: "db"}}}), nil
			})

			harness.press(tt.press...)
			harness.fireMatching(ui.IsSaysSettled)

			assert.Equal(t, tt.wantHovered, readHoveredRow(harness.readFrame()))
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
		require.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("/", "^", "a", "enter")
		assert.Equal(t, "├─ api-refactor", readHoveredRow(harness.readFrame()))

		harness.press("n")
		assert.Equal(t, "└─ auth-fix", readHoveredRow(harness.readFrame()))

		harness.press("N")
		assert.Equal(t, "├─ api-refactor", readHoveredRow(harness.readFrame()))

		harness.press("esc")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("the schedules walk their matches and narrow to them", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.watch(buildSnapshot(
			newLiveWorkflow("w1", "backend"),
			newNamedSchedule("s1", "nightly"),
			newNamedSchedule("s2", "morning"),
			newNamedSchedule("s3", "weekly"),
		))
		readSchedules := func() ([]string, string) {
			var names []string
			var hovered string
			for _, line := range readSidebar(harness.readFrame()) {
				if _, row, isRow := strings.Cut(line, "◇ "); isRow {
					names = append(names, strings.Fields(row)[0])
					if strings.HasPrefix(line, "›") {
						hovered = names[len(names)-1]
					}
				}
			}

			return names, hovered
		}
		harness.press("tab")

		harness.press("/", "l", "y", "enter")
		names, hovered := readSchedules()
		assert.Equal(t, []string{"weekly", "morning", "nightly"}, names, "a search leaves the list whole")
		assert.Equal(t, "weekly", hovered)

		harness.press("n")
		_, hovered = readSchedules()
		assert.Equal(t, "nightly", hovered, "n walks to the next match")

		harness.press("N")
		_, hovered = readSchedules()
		assert.Equal(t, "weekly", hovered, "and N walks back")

		harness.press("esc", "/", "tab", "l", "y", "enter")
		names, _ = readSchedules()
		assert.Equal(t, []string{"weekly", "nightly"}, names)
	})

	t.Run("the documents walk their matches and narrow to them", func(t *testing.T) {
		harness, _ := openDocuments(t)

		harness.press("/", "r", "o", "l", "l", "enter")
		assert.Contains(t, readHoveredRow(harness.readFrame()), "Rollout report")
		harness.fireTimers()
		assert.Contains(t, readStageText(harness.readFrame()), "It went out on Tuesday.")

		harness.press("esc", "/", "tab", "r", "o", "l", "l", "enter")
		harness.fireTimers()
		frame := harness.readFrame()
		assert.NotContains(t, readSidebarProse(frame), "Design the cache")
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
			harness := newPanelHarness(t, panelHarnessParams{})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(
				newLiveWorkflow("w1", "backend"),
				newAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "old", "w1"),
				newNamedSchedule("s1", "nightly"),
				newArchivedSchedule("s2", "monthly-gone"),
				&worktreev1.WatchedWorktree{
					Dir:      "/trees/auth",
					RepoDir:  "/home/h/komrad",
					Branch:   "agent/auth",
					Readable: true,
				},
			))

			harness.press(append(tt.press, "/", "tab", "z", "z", "z", "enter")...)

			sidebar := readSidebarProse(harness.readFrame())
			assert.Contains(t, sidebar, "nothing matches")
			if tt.idle != "" {
				assert.NotContains(t, sidebar, tt.idle)
			}
		})
	}
}

// TestFilterWorktreeHeader covers a repository's header counting every worktree under it while a filter narrows them.
func TestFilterWorktreeHeader(t *testing.T) {
	harness := newPanelHarness(t, panelHarnessParams{})
	harness.watch(buildSnapshot(
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
	harness.press("shift+tab", "esc", "f", "shift+tab", "j", "esc", "k")
	header := readBelowViews(harness.readFrame())[5]
	require.Contains(t, header, "2.0M")

	harness.press(slices.Concat([]string{"/", "tab"}, spellKeys("worktree-view"), []string{"enter", "k"})...)

	frame := harness.readFrame()
	assert.NotContains(t, frame, "agent/other", "the filter took the other worktree away")
	assert.Equal(t, header, readBelowViews(frame)[5], "and the header's size still reads the whole listing")
	assert.Contains(t, frame, "2 worktrees", "as does its count")
}

var filterableRows = []string{"backend", "├─ auth", "│  └─ api", "└─ db", "frontend", "└─ ui"}

func openFilterable(t *testing.T, from string) *panelHarness {
	t.Helper()

	harness := openAgents(t,
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		newAgentAt("auth", "w1", 5),
		newChild("api", "api", "auth"),
		newAgentAt("db", "w1", 3),
		newAgentAt("ui", "w2", 1),
	)

	harness.press("g", "g")
	for range filterableRows {
		if readHoveredRow(harness.readFrame()) == from {
			return harness
		}
		harness.press("j")
	}
	require.Equal(t, from, readHoveredRow(harness.readFrame()))

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
