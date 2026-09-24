package ui_test

import (
	"slices"
	"strings"
	"testing"

	schedulev1 "weave/proto/gen/schedule/v1"
	worktreev1 "weave/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

var filterableRows = []string{"backend", "  auth", "  └ api", "  db", "frontend", "  ui"}

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
			wantHovered: "  db",
			wantSearch:  "/ db",
		},
		{
			name:        "the row the cursor is on counts as the nearest match",
			from:        "  auth",
			press:       []string{"/", "a", "u", "t", "h", "enter"},
			wantHovered: "  auth",
			wantSearch:  "/ auth",
		},
		{
			name:        "a header matches by its own name",
			from:        "  auth",
			press:       []string{"/", "^", "f", "r", "o", "n", "t", "enter"},
			wantHovered: "frontend",
			wantSearch:  "/ ^front",
		},
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
			name:        "n does nothing before a search stands",
			from:        "  auth",
			press:       []string{"n"},
			wantHovered: "  auth",
			wantSearch:  "/",
		},
		{
			name:        "n walks forward from the row it is on",
			from:        "  auth",
			press:       []string{"/", "a", "enter", "n"},
			wantHovered: "  └ api",
			wantSearch:  "/ a",
		},
		{
			name:        "N walks back",
			from:        "  auth",
			press:       []string{"/", "a", "enter", "n", "N"},
			wantHovered: "  auth",
			wantSearch:  "/ a",
		},
		{
			name:        "a step back from the top wraps to the last match",
			from:        "backend",
			press:       []string{"/", "^", "(", "b", "a", "c", "k", "e", "n", "d", "|", "u", "i", ")", "$", "enter", "N"},
			wantHovered: "  ui",
			wantSearch:  "/ ^(backend|ui)$",
		},
		{
			name:        "a pattern nothing matches leaves the cursor where it is",
			from:        "  auth",
			press:       []string{"/", "z", "z", "z", "enter"},
			wantHovered: "  auth",
			wantSearch:  "/ zzz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFilterable(t, tt.from)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, filterableRows, readListRows(frame), "a search never takes a row off the screen")
			assert.Equal(t, tt.wantHovered, readHoveredRow(frame))
			assert.Equal(t, tt.wantSearch, readSearch(frame))
		})
	}

	t.Run("a selection survives a search", func(t *testing.T) {
		harness := openFilterable(t, "  auth")

		harness.press("v", "j", "v", "/", "d", "b", "enter", "z")

		assert.Contains(t, harness.readFrame(), "Archive 2 agents")
	})

	t.Run("the search stays on the screen it was typed on", func(t *testing.T) {
		harness := openFilterable(t, "backend")

		harness.press("/", "d", "b", "enter", "S")

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
			wantRows:    []string{"backend", "  db"},
			wantHovered: "backend",
			wantSearch:  "▽ db",
		},
		{
			name:        "a second tab puts the search back",
			from:        "backend",
			press:       []string{"/", "d", "b", "tab", "tab", "enter"},
			wantRows:    filterableRows,
			wantHovered: "  db",
			wantSearch:  "/ db",
		},
		{
			name:        "a matching parent keeps its children",
			from:        "backend",
			press:       []string{"/", "tab", "^", "a", "u", "t", "h", "$", "enter"},
			wantRows:    []string{"backend", "  auth", "  └ api"},
			wantHovered: "backend",
			wantSearch:  "▽ ^auth$",
		},
		{
			name:        "a matching child keeps its parent and the header above that",
			from:        "backend",
			press:       []string{"/", "tab", "^", "a", "p", "i", "$", "enter"},
			wantRows:    []string{"backend", "  auth", "  └ api"},
			wantHovered: "backend",
			wantSearch:  "▽ ^api$",
		},
		{
			name:        "a matching header keeps the whole group under it",
			from:        "backend",
			press:       []string{"/", "tab", "^", "b", "a", "c", "k", "e", "n", "d", "$", "enter"},
			wantRows:    []string{"backend", "  auth", "  └ api", "  db"},
			wantHovered: "backend",
			wantSearch:  "▽ ^backend$",
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
			name:        "a row it takes away hands the cursor to the nearest match forward",
			from:        "  db",
			press:       []string{"/", "u", "tab", "enter"},
			wantRows:    []string{"backend", "  auth", "  └ api", "frontend", "  ui"},
			wantHovered: "  ui",
			wantSearch:  "▽ u",
		},
		{
			name:        "a row it keeps under a matched header holds the cursor",
			from:        "  auth",
			press:       []string{"/", "b", "a", "c", "k", "e", "n", "d", "|", "a", "p", "i", "tab", "enter"},
			wantRows:    []string{"backend", "  auth", "  └ api", "  db"},
			wantHovered: "  auth",
			wantSearch:  "▽ backend|api",
		},
		{
			name:        "the last row taken away hands the cursor back up to a match",
			from:        "  ui",
			press:       []string{"/", "^", "a", "u", "tab", "enter"},
			wantRows:    []string{"backend", "  auth", "  └ api"},
			wantHovered: "  auth",
			wantSearch:  "▽ ^au",
		},
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

	t.Run("a jump to a row the filter took away turns the filter back to a search", func(t *testing.T) {
		harness := openFilterable(t, "backend +1", newHiddenAgent("sweep", "sweep"), newMark("c", "db"))

		harness.press("/", "tab", "a", "u", "t", "h", "enter", "'", "c")

		frame := harness.readFrame()
		assert.Equal(t, "  db", readHoveredRow(frame))
		assert.Equal(t, "/ auth", readSearch(frame))
		assert.NotContains(t, readListRows(frame), "  sweep"+hiddenMark, "the hidden rows stay hidden")
	})

	t.Run("a jump to a hidden row the filter keeps reveals it with the filter standing", func(t *testing.T) {
		harness := openFilterable(t, "backend +1", newHiddenAgent("sweep", "sweep"), newMark("c", "sweep"))

		harness.press("/", "tab", "s", "w", "e", "e", "p", "enter", "'", "c")

		frame := harness.readFrame()
		assert.Equal(t, "  sweep"+hiddenMark, readHoveredRow(frame))
		assert.Equal(t, "▽ sweep", readSearch(frame))
	})
}

// TestFilterScreens covers every list screen answering the same search and filter keys.
func TestFilterScreens(t *testing.T) {
	t.Run("the archive walks its matches and clears back to where it opened", func(t *testing.T) {
		harness := openArchive(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgentAt("api-refactor", "2026-07-30T09:00:00Z"),
			newArchivedAgentAt("auth-fix", "2026-07-30T08:00:00Z"),
		)
		require.Equal(t, "backend", readHoveredRow(harness.readFrame()))

		harness.press("/", "^", "a", "enter")
		assert.Equal(t, "  api-refactor", readHoveredRow(harness.readFrame()))

		harness.press("n")
		assert.Equal(t, "  auth-fix", readHoveredRow(harness.readFrame()))

		harness.press("N")
		assert.Equal(t, "  api-refactor", readHoveredRow(harness.readFrame()))

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
		listed := func() ([]string, string) {
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
		harness.press("S")

		harness.press("/", "l", "y", "enter")
		names, hovered := listed()
		assert.Equal(t, []string{"weekly", "morning", "nightly"}, names, "a search leaves the list whole")
		assert.Equal(t, "weekly", hovered)

		harness.press("n")
		_, hovered = listed()
		assert.Equal(t, "nightly", hovered, "n walks to the next match")

		harness.press("N")
		_, hovered = listed()
		assert.Equal(t, "weekly", hovered, "and N walks back")

		harness.press("esc", "/", "tab", "l", "y", "enter")
		names, _ = listed()
		assert.Equal(t, []string{"weekly", "nightly"}, names)
	})

	tests := []struct {
		name  string
		press []string
		idle  string
	}{
		{
			name: "the agents say a filter left nothing rather than that the list is empty",
		},
		{
			name:  "the archive says a filter left nothing rather than that nothing is archived",
			press: []string{"Z"},
			idle:  "nothing archived",
		},
		{
			name:  "the schedules say a filter left nothing rather than that there are none",
			press: []string{"S"},
			idle:  "no schedules yet",
		},
		{
			name:  "the worktrees say a filter left nothing rather than that there are none",
			press: []string{"t"},
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
				&worktreev1.WatchedWorktree{
					Dir:      "/trees/auth",
					RepoDir:  "/home/h/weave",
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
			RepoDir:    "/home/h/weave",
			Branch:     "agent/worktree-view",
			Readable:   true,
			SizeBytes:  proto.Int64(1 << 20),
			MeasuredAt: proto.String(archivedAt),
		},
		&worktreev1.WatchedWorktree{
			Dir:        "/trees/other",
			RepoDir:    "/home/h/weave",
			Branch:     "agent/other",
			Readable:   true,
			SizeBytes:  proto.Int64(1 << 20),
			MeasuredAt: proto.String(archivedAt),
		},
	))
	harness.press("t")
	header := splitLines(harness.readFrame())[5]
	require.Contains(t, header, "2.0M")

	harness.press(slices.Concat([]string{"/", "tab"}, spellKeys("worktree-view"), []string{"enter"})...)

	frame := harness.readFrame()
	assert.NotContains(t, frame, "agent/other", "the filter took the other worktree away")
	assert.Equal(t, header, splitLines(frame)[5], "and the header's size still reads the whole listing")
	assert.Contains(t, frame, "2 worktrees", "as does its count")
}

func openFilterable(t *testing.T, from string, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := openAgents(t, append([]proto.Message{
		newLiveWorkflow("w1", "backend"),
		newLiveWorkflow("w2", "frontend"),
		newAgentAt("auth", "w1", 5),
		newChild("api", "api", "auth"),
		newAgentAt("db", "w1", 3),
		newAgentAt("ui", "w2", 1),
	}, rows...)...)

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
