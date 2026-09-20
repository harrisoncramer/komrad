package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/lib/dates"
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
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	return model{
		frame:         layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:          config.DefaultKeybindings(),
		screen:        screenWorktrees,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "live",
						Name:       "worktree-view",
						WorkflowID: "w1",
						Harness:    "claude",
						State:      agentstore.StateThinking,
						Cwd:        "/trees/live",
					},
					alive: true,
				},
				{
					agent: agentstore.Agent{
						ID:         "occ",
						Name:       "review-607",
						WorkflowID: "w1",
						Harness:    "claude",
						Cwd:        "/trees/live",
					},
					alive: true,
				},
			},
		},
		archive: archiveState{agents: []agentstore.Agent{
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
		}},
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
// screen landing the cursor on that agent's row, an occupant included, and being spent once.
func TestApplyWorktreesSpendsTheFocusOnTheAgentsRow(t *testing.T) {
	m := worktreesModel()
	m.worktrees.focus = "occ"

	m.applyWorktrees(worktreeFixture(), "")

	assert.Equal(t, "agent:occ", m.hoveredWorktreeKey())
	assert.Empty(t, m.worktrees.focus)
}

// TestApplyWorktreesKeepsTheCursorOnItsRow covers the cursor following its row as the listing
// changes under it, and the inspections of directories that left the listing being dropped.
func TestApplyWorktreesKeepsTheCursorOnItsRow(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 6
	m.worktrees.inspected = map[string]worktreeInspection{
		"/trees/orphan": {answered: true, baseRef: "origin/main"},
		"/trees/gone":   {answered: true, baseRef: "origin/main"},
	}
	require.Equal(t, "tree:/trees/orphan", m.hoveredWorktreeKey())

	fresh := worktreeFixture()[1:]
	m.applyWorktrees(fresh, m.hoveredWorktreeKey())

	assert.Equal(t, "tree:/trees/orphan", m.hoveredWorktreeKey())
	assert.Contains(t, m.worktrees.inspected, "/trees/orphan")
	assert.NotContains(t, m.worktrees.inspected, "/trees/gone")
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
	assert.Equal(t, "tree:/trees/archived", opened.hoveredWorktreeKey())
}

// TestEnterOnAWorktreeLandsOnItsAgent covers the jump both ways: a live agent to the agents
// list, an archived one to the archive, and a directory with none staying put.
func TestEnterOnAWorktreeLandsOnItsAgent(t *testing.T) {
	t.Run("live", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 3
		require.Equal(t, "tree:/trees/live", m.hoveredWorktreeKey())

		after, _ := pressWorktrees(t, m, keyMsg("J"))

		assert.Equal(t, screenAgents, after.screen)
		assert.Equal(t, "agent:live", after.hoveredAgentKey())
	})

	t.Run("archived", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 5
		require.Equal(t, "tree:/trees/archived", m.hoveredWorktreeKey())

		after, _ := pressWorktrees(t, m, keyMsg("J"))

		assert.Equal(t, screenArchive, after.screen)
		assert.Equal(t, "agent:archived", after.hoveredArchiveKey())
	})

	t.Run("nobody's", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 6

		after, _ := pressWorktrees(t, m, keyMsg("J"))

		assert.Equal(t, screenWorktrees, after.screen)
		assert.False(t, m.canWorktreeRowOpen())
	})
}

// TestWorktreeFateDecidesTheDeletePath covers the three paths and the one stuck row that takes
// the remover's.
func TestWorktreeFateDecidesTheDeletePath(t *testing.T) {
	m := worktreesModel()
	items := worktreeFixture()

	assert.Equal(t, fateLive, m.worktreeFate(items[0]))
	assert.Equal(t, fateArchived, m.worktreeFate(items[1]))
	assert.Equal(t, fateUnowned, m.worktreeFate(items[2]))
	assert.Equal(t, fateUnowned, m.worktreeFate(items[3]))

	stuck := items[0]
	stuck.agent = &worktreeAgent{
		id:    "stuck",
		name:  "stuck",
		state: agentstore.StateFailedToCleanup,
	}
	assert.Equal(t, fateLive, m.worktreeFate(stuck), "a readable directory is its agent's however the cleanup went")
	stuck.readable = false
	assert.Equal(t, fateUnowned, m.worktreeFate(stuck))
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
		{name: "a live agent is offered the archive", cursor: 3, kind: formAgentArchive},
		{name: "an archived agent is offered the delete", cursor: 5, kind: formWorktreesDelete},
		{name: "nobody's directory is offered the delete", cursor: 6, kind: formWorktreesDelete},
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
	m.worktrees.cursor = 5

	before := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "review-column", before["name"])
	assert.Equal(t, "—", before["size"])
	assert.Equal(t, "agent/archived", before["branch"])
	assert.Equal(t, "claude, archived "+dates.Day(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)), before["agent"])
	assert.Equal(t, "weave", before["workflow"])
	assert.NotContains(t, before, "base")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/archived", inspection: worktreeInspection{
		answered:  true,
		bytes:     49 * 1024 * 1024,
		baseRef:   "origin/main",
		ahead:     2,
		behind:    12,
		modified:  []string{"a.go"},
		untracked: []string{"b.txt", "c.txt"},
	}})

	after := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "49M", after["size"])
	assert.Equal(t, "origin/main  ↑2 ↓12", after["base"])
	assert.Equal(t, "1 modified, 2 untracked", after["changes"])
}

// TestWorktreeFieldsOfAnUnreadableDirectory covers the short record for a registration git
// cannot read.
func TestWorktreeFieldsOfAnUnreadableDirectory(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 8
	require.Equal(t, "tree:/trees/broken", m.hoveredWorktreeKey())

	fields := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "/var/folders/T/repo", fields["repository"])
	assert.Equal(t, "none", fields["agent"])
	assert.NotContains(t, fields, "branch")
}

// TestWorktreeDocDrawsTheInspection covers the document's three blocks, the wait before the
// inspection lands, and an error being drawn rather than asked again.
func TestWorktreeDocDrawsTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 3

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
	m.worktrees.cursor = 5

	moved, cmd := pressWorktrees(t, m, keyMsg("j"))
	require.NotNil(t, cmd, "a move schedules a rest")
	assert.Equal(t, "tree:/trees/orphan", moved.hoveredWorktreeKey())

	inspect := moved.applyStageTick(stageTickMsg{move: moved.staging})
	assert.NotNil(t, inspect, "the rest asks for the hovered directory")

	stale := moved.applyStageTick(stageTickMsg{move: moved.staging - 1})
	assert.Nil(t, stale, "a rest the cursor moved on from asks for nothing")
}

// TestRefreshAsksForTheHoveredDirectoryOnce covers a row the screen opened on being read
// without a move, and not again while the answer is on its way or once it has come.
func TestRefreshAsksForTheHoveredDirectoryOnce(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 5

	require.NotNil(t, m.inspectHoveredWorktreeCmd())
	assert.True(t, m.worktrees.inspected["/trees/archived"].pending)
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
	m.worktrees.cursor = 5
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/archived", inspection: worktreeInspection{
		answered: true,
		baseRef:  "origin/main",
		ahead:    2,
	}})

	require.NotNil(t, m.inspectWorktreeCmd())

	assert.True(t, m.worktrees.inspected["/trees/archived"].pending)
	assert.Equal(t, "origin/main  ↑2", fieldMap(m.worktreesDetails().fields)["base"])
	assert.Contains(t, m.worktreeDoc(), "## Commits since origin/main")
	assert.NotContains(t, m.worktreeDoc(), "reading the directory")
}

// TestWorktreesTallyHeadsTheList covers the title carrying the count.
func TestWorktreesTallyHeadsTheList(t *testing.T) {
	m := worktreesModel()
	assert.Equal(t, "5", m.worktreesTally())

	m.worktrees.items = worktreeFixture()[2:4]
	assert.Equal(t, "2", m.worktreesTally())
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

	assert.Equal(t, 3, m.worktrees.cursor, "the cursor lands on the live agent's row")
	assert.True(t, m.frame.IsSolo(), "the panel draws both halves itself")
	assert.Empty(t, m.frame.GuestPane(), "no agent pane is put on the stage")
	assert.False(t, screenWorktrees.opensInfoPane(), "and the log and terminal keys do not act here")
}
