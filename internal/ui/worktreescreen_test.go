package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/lib/xslice"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// worktreesModel is the worktrees screen over the fixture listing, with the live agent on the
// agents list and the archived ones in the archive.
func worktreesModel() model {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)

	return model{
		frame:         layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:          config.DefaultKeybindings(),
		screen:        screenWorktrees,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{
				{
					agent: panel.Agent{
						ID:         "live",
						Name:       "worktree-view",
						WorkflowID: "w1",
						Harness:    "claude",
						State:      panel.StateThinking,
						Cwd:        "/trees/live",
					},
					alive: true,
				},
				{
					agent: panel.Agent{
						ID:         "occ",
						Name:       "review-607",
						WorkflowID: "w1",
						Harness:    "claude",
						Cwd:        "/trees/live",
					},
					alive: true,
				},
			},
			archivedAgents: archivedItems([]panel.Agent{
				{
					ID:         "archived",
					Name:       "review-column",
					WorkflowID: "w1",
					Harness:    "claude",
					ArchivedAt: &archived,
					Cwd:        "/trees/archived",
				},
				{
					ID:         "admin",
					Name:       "custom-cargo-fixes",
					WorkflowID: "w1",
					Harness:    "claude",
					ArchivedAt: &archived,
					Cwd:        "/trees/admin",
				},
			}),
		},
		worktrees: worktreesState{items: worktreeFixture()},
	}
}

func pressWorktrees(t *testing.T, m model, key tea.KeyMsg) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.updateWorktrees(key)
	updated, ok := next.(model)
	require.True(t, ok)

	return updated, cmd
}

// TestApplyWorktreesSpendsTheFocusOnTheAgentsRow covers a focus set by the key that opened the
// screen landing the cursor on that agent's directory, an occupant's included, and being spent
// once.
func TestApplyWorktreesSpendsTheFocusOnTheAgentsRow(t *testing.T) {
	m := worktreesModel()
	m.worktrees.focus = "occ"

	m.applyWorktrees(worktreeFixture(), "")

	assert.Equal(t, "tree:/trees/live", worktreesList().hovered(m))
	assert.Empty(t, m.worktrees.focus)
}

// TestApplyWorktreesKeepsTheCursorOnItsRow covers the cursor following its row as the listing
// changes under it, and the inspections of directories that left the listing being dropped.
func TestApplyWorktreesKeepsTheCursorOnItsRow(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 5
	m.inspections = map[string]worktreeInspection{
		"/trees/orphan": {answered: true, baseRef: "origin/main"},
		"/trees/gone":   {answered: true, baseRef: "origin/main"},
	}
	require.Equal(t, "tree:/trees/orphan", worktreesList().hovered(m))

	fresh := worktreeFixture()[1:]
	m.applyWorktrees(fresh, worktreesList().hovered(m))

	assert.Equal(t, "tree:/trees/orphan", worktreesList().hovered(m))
	assert.Contains(t, m.inspections, "/trees/orphan")
	assert.NotContains(t, m.inspections, "/trees/gone")
}

// TestOpenWorktreesLandsOnTheHoveredAgent covers the key from the agents list entering the
// screen with the cursor already on that agent's directory.
func TestOpenWorktreesLandsOnTheHoveredAgent(t *testing.T) {
	m := worktreesModel()
	m.screen = screenAgents

	next, _ := m.openWorktrees("archived")
	opened, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, screenWorktrees, opened.screen)
	assert.Equal(t, "tree:/trees/archived", worktreesList().hovered(opened))
}

// TestEnterOnAWorktreeLandsOnItsAgent covers the jump both ways: a live agent to the agents
// list, an archived one to the archive, and a directory with none staying put.
func TestEnterOnAWorktreeLandsOnItsAgent(t *testing.T) {
	t.Run("live", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 4
		require.Equal(t, "tree:/trees/live", worktreesList().hovered(m))

		after, _ := pressWorktrees(t, m, keyMsg("J"))

		assert.Equal(t, screenAgents, after.screen)
		assert.Equal(t, "agent:live", agentsList().hovered(after))
	})

	t.Run("archived", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 3
		require.Equal(t, "tree:/trees/archived", worktreesList().hovered(m))

		after, _ := pressWorktrees(t, m, keyMsg("J"))

		assert.Equal(t, screenArchive, after.screen)
		assert.Equal(t, "agent:archived", archiveList().hovered(after))
	})

	t.Run("nobody's", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 5
		require.Equal(t, "tree:/trees/orphan", worktreesList().hovered(m))

		after, _ := pressWorktrees(t, m, keyMsg("J"))

		assert.Equal(t, screenWorktrees, after.screen)
		assert.False(t, m.canWorktreeRowOpen())
	})
}

// TestWorktreeFateIsReadOffTheWire covers each fate the server names landing on the item, and
// one it does not name reading as unowned.
func TestWorktreeFateIsReadOffTheWire(t *testing.T) {
	for wire, want := range map[worktreev1.WorktreeFate]worktreeFate{
		worktreev1.WorktreeFate_WORKTREE_FATE_LIVE:        fateLive,
		worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED:    fateArchived,
		worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED:     fateUnowned,
		worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED: fateUnowned,
	} {
		item := worktreeItemFrom(&worktreev1.Worktree{Dir: "/trees/a", Fate: wire})
		assert.Equal(t, want, item.fate, wire.String())
	}
}

// TestDeleteOnAWorktreeOpensTheFormItsFateNames covers the three keys: the archive choice on a
// live row, the delete form on an archived one and on one nothing answers for, and nothing on
// a header.
func TestDeleteOnAWorktreeOpensTheFormItsFateNames(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		kind   formKind
	}{
		{name: "a header opens nothing", cursor: 2, kind: formNone},
		{name: "a live agent is offered the archive", cursor: 4, kind: formAgentArchive},
		{name: "an archived agent is offered the delete", cursor: 3, kind: formWorktreesDelete},
		{name: "nobody's directory is offered the delete", cursor: 5, kind: formWorktreesDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			m.width, m.height = 200, 40
			m.worktrees.cursor = tt.cursor

			after, _ := pressWorktrees(t, m, keyMsg("d"))

			assert.Equal(t, tt.kind, after.form.kind)
		})
	}
}

// TestClearWorktreesTakesEverythingButTheLive covers the clear key gathering the archived and
// the unowned directories and leaving the live one alone.
func TestClearWorktreesTakesEverythingButTheLive(t *testing.T) {
	m := worktreesModel()
	m.width, m.height = 200, 40

	after, _ := pressWorktrees(t, m, keyMsg("D"))

	require.Equal(t, formWorktreesDelete, after.form.kind)
	assert.Contains(t, formText(after.form.form), "Delete 4 worktrees?")
}

// TestDeleteChosenWorktreesRefusesALiveRow covers a bulk delete over a selection holding a
// live agent being refused in the message region rather than archiving anything.
func TestDeleteChosenWorktreesRefusesALiveRow(t *testing.T) {
	m := worktreesModel()
	m.width, m.height = 200, 40
	m.worktrees.selected = selection{keys: []string{"tree:/trees/live", "tree:/trees/orphan"}}

	after, _ := pressWorktrees(t, m, keyMsg("d"))

	assert.Equal(t, formNone, after.form.kind)
	require.NotEmpty(t, after.messages.items)
	assert.Contains(t, after.messages.items[0].summary, "live agent")
}

// recordingWorktrees answers the queued delete and keeps the directories the panel named.
type recordingWorktrees struct {
	worktreev1connect.WorktreeServiceClient
	queued [][]string
	err    error
}

func (r *recordingWorktrees) ListWorktrees(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[worktreev1.ListWorktreesResponse], error) {
	return connect.NewResponse(&worktreev1.ListWorktreesResponse{}), nil
}

func (r *recordingWorktrees) DeleteWorktrees(_ context.Context, req *connect.Request[worktreev1.DeleteWorktreesRequest]) (*connect.Response[emptypb.Empty], error) {
	r.queued = append(r.queued, req.Msg.GetDirs())
	if r.err != nil {
		return nil, r.err
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// TestConfirmingAWorktreeDeleteQueuesOneJob covers the clear key's prompt sending every
// directory that can go as one call, the archived and the unowned alike, the queued job being
// announced, and a refusal landing in the message region.
func TestConfirmingAWorktreeDeleteQueuesOneJob(t *testing.T) {
	tests := []struct {
		name       string
		answer     string
		err        error
		wantQueued [][]string
		wantErr    bool
	}{
		{
			name:    "a queue that refuses is reported",
			answer:  "y",
			err:     errors.New("the queue is away"),
			wantErr: true,
			wantQueued: [][]string{{
				"/trees/admin",
				"/trees/archived",
				"/trees/orphan",
				"/trees/broken",
			}},
		},
		{
			name:   "declining queues nothing",
			answer: "n",
		},
		{
			name:   "confirming queues the directories in one call",
			answer: "y",
			wantQueued: [][]string{{
				"/trees/admin",
				"/trees/archived",
				"/trees/orphan",
				"/trees/broken",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &recordingWorktrees{err: tt.err}
			m := worktreesModel()
			m.width, m.height = 200, 40
			m.api = api{
				agents:    emptyLists{},
				workflows: emptyLists{},
				schedules: emptyLists{},
				worktrees: client,
			}

			opened, _ := pressWorktrees(t, m, keyMsg("D"))
			require.Equal(t, formWorktreesDelete, opened.form.kind)

			got := answerPrompt(t, opened, tt.answer)

			assert.Equal(t, tt.wantQueued, client.queued)
			assert.False(t, got.form.isActive(), "the prompt is down")
			switch {
			case tt.wantErr:
				require.NotEmpty(t, got.messages.items)
				assert.Equal(t, messageError, got.messages.items[0].kind)
				assert.Contains(t, got.messages.items[0].summary, "Could not delete 4 worktrees")
			case tt.wantQueued != nil:
				require.NotEmpty(t, got.messages.items, "the queued delete is announced")
				assert.Equal(t, messageInfo, got.messages.items[0].kind)
				assert.Equal(t, "Deleting 4 worktrees", got.messages.items[0].summary)
			default:
				assert.Empty(t, got.messages.items)
			}
		})
	}
}

// TestDeleteWorktreesFormSaysWhatGoes covers the title naming one directory or counting them,
// and the branch line following the inspection.
func TestDeleteWorktreesFormSaysWhatGoes(t *testing.T) {
	items := worktreeFixture()
	kept := true

	form, _ := deleteWorktreesForm(worktreeDeletion{agents: items[1:2]}, &kept, config.FixedFormKeys())
	drawn := formText(form)
	assert.Contains(t, drawn, "Delete the worktree of review-column?")
	assert.Contains(t, drawn, "The branch is kept")

	form, _ = deleteWorktreesForm(worktreeDeletion{agents: items[1:2], unowned: items[2:4]}, nil, config.FixedFormKeys())
	drawn = formText(form)
	assert.Contains(t, drawn, "Delete 3 worktrees?")
	assert.Contains(t, drawn, "2 directories nothing")
	assert.NotContains(t, drawn, "branch is kept")

	form, _ = deleteWorktreesForm(worktreeDeletion{unowned: items[3:4]}, nil, config.FixedFormKeys())
	assert.Contains(t, formText(form), "Delete the worktree of broken?")
}

// formText is a form as it draws once it has been given a window, without the styling.
func formText(form *huh.Form) string {
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return ansi.Strip(form.View())
}

// TestWorktreeFieldsReadTheListingAndTheInspection covers the record drawn at once from the
// listing and filled in once the inspection lands, with the agent named through the listing
// that holds it.
func TestWorktreeFieldsReadTheListingAndTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 3
	require.Equal(t, "tree:/trees/archived", worktreesList().hovered(m))

	before := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "review-column", before["name"])
	assert.Equal(t, "—", before["size"])
	assert.Equal(t, "—", before["last active"])
	assert.Equal(t, "agent/archived", before["branch"])
	assert.Equal(t, "claude, archived "+label.Day(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)), before["agent"])
	assert.Equal(t, "weave", before["workflow"])
	assert.NotContains(t, before, "base")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/archived", inspection: worktreeInspection{
		answered:  true,
		baseRef:   "origin/main",
		ahead:     2,
		behind:    12,
		modified:  []string{"a.go"},
		untracked: []string{"b.txt", "c.txt"},
	}})

	after := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "origin/main  ↑2 ↓12", after["base"])
	assert.Equal(t, "1 modified, 2 untracked", after["changes"])
}

// TestWorktreeFieldsSayWhatTheDirectoryTakesAndWhenItWasActive covers the size line carrying the
// measurement and its age, explaining a failed walk, and the last active line carrying the age
// and whether the agent was put away.
func TestWorktreeFieldsSayWhatTheDirectoryTakesAndWhenItWasActive(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	archived := now.Add(-4 * 24 * time.Hour)
	items := worktreeFixture()

	tests := []struct {
		name           string
		item           worktreeItem
		wantSize       string
		wantLastActive string
	}{
		{
			name:           "nothing measured and nothing touched",
			item:           items[2],
			wantSize:       "—",
			wantLastActive: "—",
		},
		{
			name: "a failed walk",
			item: worktreeItem{
				dir:          "/trees/orphan",
				readable:     true,
				lastActiveAt: now.Add(-90 * time.Minute),
				size: &worktreeSize{
					measuredAt: now.Add(-5 * time.Minute),
					err:        "permission denied",
				},
			},
			wantSize:       "—, could not measure: permission denied",
			wantLastActive: "1h ago",
		},
		{
			name: "a measured directory whose agent was put away",
			item: worktreeItem{
				dir:      "/trees/archived",
				readable: true,
				agent: &worktreeAgent{
					id:         "archived",
					name:       "review-column",
					archivedAt: &archived,
				},
				lastActiveAt: archived,
				size: &worktreeSize{
					bytes:      49 * 1024 * 1024,
					measuredAt: now.Add(-5 * time.Minute),
				},
			},
			wantSize:       "49M, measured 5m ago",
			wantLastActive: "4d ago, archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := fieldMap(worktreesModel().worktreeFields(tt.item, now))

			assert.Equal(t, tt.wantSize, fields["size"])
			assert.Equal(t, tt.wantLastActive, fields["last active"])
		})
	}
}

// TestWorktreeFieldsNameTheOccupants covers the record naming the other agents working in a
// directory after its branch, or after its repository where git cannot read it, and saying
// nothing where there are none.
func TestWorktreeFieldsNameTheOccupants(t *testing.T) {
	tests := []struct {
		name       string
		item       worktreeItem
		occupants  []worktreeAgent
		want       string
		wantLabels []string
	}{
		{
			name:       "none is left out",
			item:       worktreeFixture()[0],
			occupants:  nil,
			want:       "",
			wantLabels: []string{"name", "directory", "size", "last active", "branch", "repository", "agent", "workflow"},
		},
		{
			name:       "one is named",
			item:       worktreeFixture()[0],
			occupants:  []worktreeAgent{{id: "occ", name: "review-607"}},
			want:       "review-607",
			wantLabels: []string{"name", "directory", "size", "last active", "branch", "repository", "also here", "agent", "workflow"},
		},
		{
			name: "two are named in the listing's order",
			item: worktreeFixture()[0],
			occupants: []worktreeAgent{
				{id: "occ", name: "review-607"},
				{id: "reader", name: "second-reader"},
			},
			want:       "review-607, second-reader",
			wantLabels: []string{"name", "directory", "size", "last active", "branch", "repository", "also here", "agent", "workflow"},
		},
		{
			name:       "an unreadable directory names them after its repository",
			item:       worktreeFixture()[3],
			occupants:  []worktreeAgent{{id: "occ", name: "review-607"}},
			want:       "review-607",
			wantLabels: []string{"name", "directory", "size", "last active", "repository", "also here", "agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			item := tt.item
			item.occupants = tt.occupants

			fields := m.worktreeFields(item, time.Now())

			assert.Equal(t, tt.want, fieldMap(fields)["also here"])
			assert.Equal(t, tt.wantLabels, xslice.Map(fields, func(field recordField) string { return field.label }))
		})
	}
}

// TestWorktreeFieldsOfAnUnreadableDirectory covers the short record for a registration git
// cannot read.
func TestWorktreeFieldsOfAnUnreadableDirectory(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 7
	require.Equal(t, "tree:/trees/broken", worktreesList().hovered(m))

	fields := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "/var/folders/T/repo", fields["repository"])
	assert.Equal(t, "none", fields["agent"])
	assert.NotContains(t, fields, "branch")
}

// TestWorktreeDocDrawsTheInspection covers the document's three blocks, the wait before the
// inspection lands, and an error being drawn rather than asked again.
func TestWorktreeDocDrawsTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 4
	require.Equal(t, "tree:/trees/live", worktreesList().hovered(m))

	assert.Contains(t, m.worktreeDoc(), "reading the directory")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/live", inspection: worktreeInspection{
		answered: true,
		entries: []worktreeEntry{
			{
				path:  "local",
				bytes: 40 * 1024 * 1024,
				dir:   true,
			},
			{path: "go.mod", bytes: 2048},
		},
		modified:  []string{"a.go"},
		untracked: []string{"b.txt"},
		baseRef:   "origin/main",
		commits: []worktreeCommit{{
			sha:         "87a3bcc98c47",
			subject:     "fix(view): drop the column",
			committedAt: time.Now().Add(-2 * time.Hour),
		}},
	}})

	doc := m.worktreeDoc()
	assert.Contains(t, doc, "# worktree-view")
	assert.Contains(t, doc, "local/")
	assert.Contains(t, doc, "40M")
	assert.Contains(t, doc, "M a.go")
	assert.Contains(t, doc, "? b.txt")
	assert.Contains(t, doc, "## Commits since origin/main")
	assert.Contains(t, doc, "87a3bcc98c47  2h   fix(view): drop the column")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/live", inspection: worktreeInspection{answered: true, err: errors.New("git exploded")}})
	assert.Contains(t, m.worktreeDoc(), "git exploded")
}

// TestEntryLinesCutTheListAndCountTheRest covers the entries beyond the first ten folding
// into one line with their total.
func TestEntryLinesCutTheListAndCountTheRest(t *testing.T) {
	entries := make([]worktreeEntry, 0, 12)
	for i := range 12 {
		entries = append(entries, worktreeEntry{path: strings.Repeat("x", i+1), bytes: 1024})
	}

	lines := entryLines(entries)

	require.Len(t, lines, 11)
	assert.Contains(t, lines[10], "+ 2 more")
	assert.Contains(t, lines[10], "2.0K")
}

// TestStageTickOnTheWorktreesScreenAsksForTheInspection covers the rest of the cursor being
// what sends get_worktree, not the move itself.
func TestStageTickOnTheWorktreesScreenAsksForTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 4

	moved, cmd := pressWorktrees(t, m, keyMsg("j"))
	require.NotNil(t, cmd, "a move schedules a rest")
	assert.Equal(t, "tree:/trees/orphan", worktreesList().hovered(moved))

	inspect := moved.applyStageTick(stageTickMsg{move: moved.staging})
	assert.NotNil(t, inspect, "the rest asks for the hovered directory")

	stale := moved.applyStageTick(stageTickMsg{move: moved.staging - 1})
	assert.Nil(t, stale, "a rest the cursor moved on from asks for nothing")
}

// TestRefreshAsksForTheHoveredDirectoryOnce covers a row the screen opened on being read
// without a move, and not again while the answer is on its way or once it has come.
func TestRefreshAsksForTheHoveredDirectoryOnce(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 3
	require.Equal(t, "tree:/trees/archived", worktreesList().hovered(m))

	require.NotNil(t, m.inspectHoveredWorktreeCmd())
	assert.True(t, m.inspections["/trees/archived"].pending)
	assert.Nil(t, m.inspectHoveredWorktreeCmd(), "an answer on its way is not asked for again")
	assert.Nil(t, m.inspectWorktreeCmd(), "not even by a rest of the cursor")
	assert.Contains(t, m.worktreeDoc(), "reading the directory")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/archived", inspection: worktreeInspection{answered: true, err: errors.New("refused")}})
	assert.Nil(t, m.inspectHoveredWorktreeCmd(), "an answer, even a refusal, is not asked for again")
	assert.NotNil(t, m.inspectWorktreeCmd(), "a rest of the cursor reads it afresh")

	m.screen = screenAgents
	assert.Nil(t, m.inspectHoveredWorktreeCmd())
}

// TestARowReturnedToKeepsItsAnswerWhileItIsReadAgain covers the fresh reading a rest asks for
// leaving the last answer on the record and the document until it lands.
func TestARowReturnedToKeepsItsAnswerWhileItIsReadAgain(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 3
	require.Equal(t, "tree:/trees/archived", worktreesList().hovered(m))
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/archived", inspection: worktreeInspection{
		answered: true,
		baseRef:  "origin/main",
		ahead:    2,
	}})

	require.NotNil(t, m.inspectWorktreeCmd())

	assert.True(t, m.inspections["/trees/archived"].pending)
	assert.Equal(t, "origin/main  ↑2", fieldMap(m.worktreesDetails().fields)["base"])
	assert.Contains(t, m.worktreeDoc(), "## Commits since origin/main")
	assert.NotContains(t, m.worktreeDoc(), "reading the directory")
}

// TestWorktreesTallyHeadsTheList covers the title carrying the count and the total of what has
// been measured across every group, and a dash while nothing has.
func TestWorktreesTallyHeadsTheList(t *testing.T) {
	m := worktreesModel()
	assert.Equal(t, "5 · —", m.worktreesTally())

	m.worktrees.items = measuredFixture(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	assert.Equal(t, "5 · 1.6G", m.worktreesTally())
}

func fieldMap(fields []recordField) map[string]string {
	byKey := map[string]string{}
	for _, field := range fields {
		byKey[field.label] = field.value
	}

	return byKey
}

// TestWorktreesScreenHoldsTheWindow covers the panel keeping the whole window on this screen,
// a live agent's pane included, the way the archive does.
func TestWorktreesScreenHoldsTheWindow(t *testing.T) {
	tmux := &recordingTmux{}
	m := worktreesModel()
	m.screen = screenAgents
	m.tmux, m.frame = tmux, layout.New(tmux, config.DefaultLayout())
	m = sizedTo(m, tmux)
	m.listing.agents[0].agent.Pane = "%7"

	entered, _ := m.openWorktrees("live")
	m, ok := entered.(model)
	require.True(t, ok)

	assert.Equal(t, 4, m.worktrees.cursor, "the cursor lands on the live agent's row")
	assert.True(t, m.frame.IsSolo(), "the panel draws both halves itself")
	assert.Empty(t, m.frame.GuestPane(), "no agent pane is put on the stage")
	assert.False(t, screenWorktrees.opensInfoPane(), "and the log and terminal keys do not act here")
}
