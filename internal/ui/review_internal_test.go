package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/diff"
	"weave/internal/gittest"
	"weave/internal/ui/diffview"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	watchv1 "weave/proto/gen/watch/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingReviewAgents struct {
	agentv1connect.AgentServiceClient
	repo    *gittest.Repo
	rounds  []*agentv1.ReviewRound
	reviews []*agentv1.Review
	threads []*agentv1.ReviewThread
	created *agentv1.CreateReviewThreadRequest
	held    *agentv1.HoldAgentRequest
	letGo   *agentv1.ReleaseAgentRequest
}

// TestReviewInFlight covers a load or a write landing after the screen has moved on under it.
func TestReviewInFlight(t *testing.T) {
	t.Run("the frame draws while the diff loads", func(t *testing.T) {
		m, _ := buildReviewModel(t, &recordingReviewAgents{})
		m.helpExpanded, m.height = true, 40

		cmd := m.enterReview(m.listing.agents[0].Agent)
		require.NotNil(t, cmd)

		shown := ansi.Strip(m.review.view(m.shared()))
		assert.Contains(t, shown, "loading the diff")
		assert.Contains(t, shown, "start a review here")
	})

	t.Run("a composer goes with the diff a landing load takes away", func(t *testing.T) {
		agents := &recordingReviewAgents{}
		m := buildReviewWithAnEarlierReview(t, agents)
		m = pressReviewBase(t, m)
		m.review.diff.cursor = findRowWithAnAnchor(t, m)
		next, _ := m.routeScreenMessage(keyMsg("c"))
		m = typeReviewMessage(t, next.(model), "this should say hello")

		loaded, ok := m.review.reload(m.shared())().(reviewLoadedMsg)
		require.True(t, ok)
		loaded.chapters = nil
		m.review.applyLoaded(&m, m.shared(), loaded)

		assert.False(t, m.review.threads.composing)
		m.routeScreenMessage(keyMsg("ctrl+s"))
		assert.Nil(t, agents.created)
	})

	t.Run("another write landing mid send is not the send", func(t *testing.T) {
		agents := &recordingReviewAgents{}
		m, _ := buildReviewModel(t, agents)
		m = openReview(t, m)
		m = pressEdit(t, m)
		m.review.closeEdit(&m, m.shared())

		require.NotNil(t, m.review.send(m.shared(), false))
		letGo := m.review.applyWrote(&m, m.shared(), reviewWroteMsg{agentID: m.review.agentID, notice: "comment added"})

		assert.Nil(t, letGo)
		assert.Nil(t, agents.letGo)
		assert.True(t, m.review.pass.paused)
	})
}

// TestReviewWhileTheAgentStops covers the keys pressed between asking the agent to stop and its answer.
func TestReviewWhileTheAgentStops(t *testing.T) {
	t.Run("a second edit press waits for the first", func(t *testing.T) {
		m, _ := buildReviewModel(t, &recordingReviewAgents{})
		m = openReview(t, m)

		next, cmd := m.routeScreenMessage(keyMsg("e"))
		m = next.(model)
		assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "reviewing · stopping the age")

		next, _ = m.routeScreenMessage(keyMsg("e"))
		m = next.(model)
		assert.False(t, m.review.pass.atEditor)
		assert.Contains(t, m.review.notice, "still stopping")

		m = settlePause(t, m, cmd)
		assert.True(t, m.review.pass.atEditor)
	})

	t.Run("a pass given up before the agent stops lets it go", func(t *testing.T) {
		agents := &recordingReviewAgents{}
		m, _ := buildReviewModel(t, agents)
		m = openReview(t, m)
		setReviewAgentState(&m, panel.StateThinking)

		next, cmd := m.routeScreenMessage(keyMsg("e"))
		next, _ = next.(model).routeScreenMessage(keyMsg("esc"))
		m = next.(model)
		require.Equal(t, modeWatching, m.review.pass.mode)

		next, letGo := m.Update(cmd())
		m = next.(model)
		assert.False(t, m.review.pass.atEditor)
		require.NotNil(t, letGo)
		letGo()
		require.NotNil(t, agents.letGo)
		assert.Equal(t, "a1", agents.letGo.GetId())
	})

	t.Run("dropping the review before the agent stops lets it go", func(t *testing.T) {
		tests := []struct {
			name string
			key  string
		}{
			{
				name: "an edit waiting on the hold",
				key:  "e",
			},
			{
				name: "a comment waiting on the hold",
				key:  "c",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				agents := &recordingReviewAgents{}
				m, _ := buildReviewModel(t, agents)
				m = openReview(t, m)
				setReviewAgentState(&m, panel.StateThinking)

				next, cmd := m.routeScreenMessage(keyMsg(tt.key))
				m = next.(model)
				for m.screen == screenReview {
					next, _ = m.Update(keyMsg("esc"))
					m = next.(model)
				}

				_, letGo := m.Update(findPaused(t, cmd()))
				require.NotNil(t, agents.held)
				require.NotNil(t, letGo)
				letGo()
				require.NotNil(t, agents.letGo)
				assert.Equal(t, "a1", agents.letGo.GetId())
			})
		}
	})
}

// TestReviewEditor covers the editor pane the edit key opens.
func TestReviewEditor(t *testing.T) {
	t.Run("the editor quitting gives the stage back and reads the diff again", func(t *testing.T) {
		m, tmux := buildReviewModel(t, &recordingReviewAgents{})
		m = openReview(t, m)
		m = pressEdit(t, m)
		require.NotEmpty(t, m.review.pass.editorPane)

		tmux.deadPanes = map[string]bool{m.review.pass.editorPane: true}
		cmd := m.reconcileEditor()

		assert.Equal(t, modeReviewing, m.review.pass.mode)
		assert.False(t, m.review.pass.atEditor)
		assert.NotNil(t, cmd)
	})

	t.Run("the editor opens at the repository root the paths are named from", func(t *testing.T) {
		agents := &recordingReviewAgents{}
		m, tmux := buildReviewModel(t, agents)
		root := agents.repo.Root
		m.settings.Editor = "vi"
		m.listing.agents[0].Agent.Cwd = filepath.Join(root, "cmd", "greet")
		m = openReview(t, m)

		pressEdit(t, m)

		require.NotEmpty(t, tmux.respawnDirs)
		assert.Equal(t, root, tmux.respawnDirs[0])
		assert.Contains(t, tmux.respawns[0], "'greet.go'")
	})
}

// TestIsReviewHistory covers a comment being refused on the snapshot the diff was drawn to rather than the round asked for.
func TestIsReviewHistory(t *testing.T) {
	tests := []struct {
		name string
		diff reviewDiff
		want bool
	}{
		{
			name: "the chapter list was recut under an index that stood still",
			diff: reviewDiff{
				chapters: []reviewChapter{{ToSHA: "s3", Ordinal: 3}},
				head:     "s2",
			},
			want: true,
		},
		{
			name: "the base and head were cleared under a diff still drawn to an older round",
			diff: reviewDiff{
				chapter: -1,
				view:    diffview.NewView(&diff.Patch{BaseSHA: "s1", SnapshotSHA: "s2"}, false),
			},
			want: true,
		},
		{
			name: "the diff stands on the round asked for",
			diff: reviewDiff{
				chapters: []reviewChapter{{ToSHA: "s3", Ordinal: 3}},
				head:     "s3",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			screen := reviewScreen{
				round: &agentv1.ReviewRound{Id: "r3", SnapshotSha: "s3"},
				diff:  tt.diff,
			}

			said, history := screen.isHistory()

			assert.Equal(t, tt.want, history)
			assert.Equal(t, tt.want, said != "")
		})
	}
}

// TestReviewEmphasis covers what the review draws in colour or weight rather than in text.
func TestReviewEmphasis(t *testing.T) {
	t.Run("the owed list brightens the comments drawn beside it", func(t *testing.T) {
		agents := &recordingReviewAgents{
			threads: []*agentv1.ReviewThread{{
				Id:       "t1",
				ReviewId: "r1",
				FilePath: gittest.File,
				NewStart: 2,
				Messages: []*agentv1.ReviewMessage{{
					Id:         "m1",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
				}},
			}},
			reviews: []*agentv1.Review{{
				Id:          "r1",
				AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
				ReleasedAt:  "2026-09-13T10:00:00Z",
				OpenThreads: 1,
			}},
		}
		m, _ := buildReviewModel(t, agents)
		m = openReview(t, m)
		bright := paint.Panel.Style(reviewOwedTone).Bold(true).Render(gittest.File + ":2")
		require.Contains(t, m.review.sidebarView(m.shared()), bright)

		m.review.diff.top = m.review.diff.view.Len() + 100

		assert.NotContains(t, m.review.sidebarView(m.shared()), bright)
		assert.Contains(t, m.review.sidebarView(m.shared()), dimStyle.Render(gittest.File+":2"))
	})

	t.Run("the chosen verdict wears its own tone rather than the muted one", func(t *testing.T) {
		m, _ := buildReviewModel(t, &recordingReviewAgents{})
		m = openReview(t, m)

		next, _ := m.routeScreenMessage(keyMsg("ctrl+s"))
		m = next.(model)

		shown := m.review.releaseView(m.shared())
		verdict := reviewVerdicts[m.review.threads.verdict]
		chosen := "  ▸ " + describeVerdict(verdict)
		assert.Contains(t, shown, paint.Panel.Style(verdictTone(verdict)).Render(chosen))
		assert.NotContains(t, shown, paint.Panel.Style(paint.ToneMuted).Render(chosen))
	})

	t.Run("the cursor row dims while the record has the keys", func(t *testing.T) {
		m, _ := buildReviewModel(t, &recordingReviewAgents{})
		m = openReview(t, m)
		live := findCursorRow(t, m)

		next, _ := m.Update(keyMsg("ctrl+t"))
		m = next.(model)
		require.True(t, m.isDetailsFocused())
		assert.NotEqual(t, live, findCursorRow(t, m))

		next, _ = m.Update(keyMsg("ctrl+t"))
		m = next.(model)
		assert.Equal(t, live, findCursorRow(t, m))
	})
}

func (f *recordingReviewAgents) HoldAgent(
	_ context.Context,
	req *connect.Request[agentv1.HoldAgentRequest],
) (*connect.Response[agentv1.HoldAgentResponse], error) {
	f.held = req.Msg

	return connect.NewResponse(&agentv1.HoldAgentResponse{}), nil
}

func (f *recordingReviewAgents) ReleaseAgent(
	_ context.Context,
	req *connect.Request[agentv1.ReleaseAgentRequest],
) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	f.letGo = req.Msg

	return connect.NewResponse(&agentv1.ReleaseAgentResponse{}), nil
}

func (f *recordingReviewAgents) ListReviewViewedFiles(
	context.Context,
	*connect.Request[agentv1.ListReviewViewedFilesRequest],
) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error) {
	return connect.NewResponse(&agentv1.ListReviewViewedFilesResponse{}), nil
}

func (f *recordingReviewAgents) OpenReviewRound(
	_ context.Context,
	req *connect.Request[agentv1.OpenReviewRoundRequest],
) (*connect.Response[agentv1.OpenReviewRoundResponse], error) {
	sha := gittest.NameSnapshot(f.repo.Tree)
	if len(f.rounds) == 0 || f.rounds[len(f.rounds)-1].GetSnapshotSha() != sha {
		f.rounds = append(f.rounds, &agentv1.ReviewRound{
			Id:          fmt.Sprintf("round-%d", len(f.rounds)+1),
			AgentId:     req.Msg.GetAgentId(),
			Ordinal:     int32(len(f.rounds) + 1),
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: sha,
		})
	}

	return connect.NewResponse(&agentv1.OpenReviewRoundResponse{Round: f.rounds[len(f.rounds)-1]}), nil
}

func (f *recordingReviewAgents) ListReviewRounds(
	context.Context,
	*connect.Request[agentv1.ListReviewRoundsRequest],
) (*connect.Response[agentv1.ListReviewRoundsResponse], error) {
	return connect.NewResponse(&agentv1.ListReviewRoundsResponse{Rounds: f.rounds}), nil
}

func (f *recordingReviewAgents) ListReviews(
	context.Context,
	*connect.Request[agentv1.ListReviewsRequest],
) (*connect.Response[agentv1.ListReviewsResponse], error) {
	return connect.NewResponse(&agentv1.ListReviewsResponse{Reviews: f.reviews}), nil
}

func (f *recordingReviewAgents) CreateReviewThread(
	_ context.Context,
	req *connect.Request[agentv1.CreateReviewThreadRequest],
) (*connect.Response[agentv1.CreateReviewThreadResponse], error) {
	f.created = req.Msg

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{Thread: &agentv1.ReviewThread{Id: "t1"}}), nil
}

func buildReviewModel(t *testing.T, agents *recordingReviewAgents) (model, *recordingTmux) {
	t.Helper()

	if agents.repo == nil {
		agents.repo = gittest.NewRepo(t.TempDir())
	}

	tmux := &recordingTmux{
		width:  200,
		height: 30,
	}
	m := infoModel(tmux)
	m.api = api{agents: agents}
	m.git = agents.repo
	m.width, m.height = 200, 30
	m.listing = listing{
		workflows: []panel.Workflow{{
			ID:       "w1",
			Name:     "backend",
			Worktree: panel.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		}},
		agents: []panel.AgentRow{{
			Agent: buildReviewAgent(agents.repo.Root),
			Alive: true,
		}},
	}

	return m, tmux
}

func buildReviewAgent(dir string) panel.Agent {
	return panel.Agent{
		ID:         "a1",
		Name:       "auth",
		Cwd:        dir,
		WorkflowID: "w1",
		Pane:       "%9",
		State:      panel.StateIdle,
	}
}

func buildReviewWithAnEarlierReview(t *testing.T, agents *recordingReviewAgents) model {
	t.Helper()

	m, _ := buildReviewModel(t, agents)
	m = openReview(t, m)
	agents.reviews = []*agentv1.Review{{
		Id:           "v1",
		RoundId:      "round-1",
		RoundOrdinal: 1,
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T10:00:00Z",
	}}
	agents.repo.Wrote(gittest.WroteParams{
		Tree:  "tree-with-a-second-file",
		Whole: gittest.PatchOfBothFiles,
		Since: gittest.PatchOfTheSecondFile,
		Sources: map[string]string{
			gittest.File: gittest.HeadSource,
			"second.go":  "package greet\n\nfunc Second() {}\n",
		},
	})

	return openReview(t, m)
}

func openReview(t *testing.T, m model) model {
	t.Helper()

	putServedRowsInWorld(&m)
	cmd := m.enterReview(m.listing.agents[0].Agent)
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	m.review.applyLoaded(&m, m.shared(), loaded)
	require.Equal(t, screenReview, m.screen)

	return m
}

func putServedRowsInWorld(m *model) {
	fake, ok := m.api.agents.(*recordingReviewAgents)
	if !ok {
		return
	}

	for _, review := range fake.reviews {
		m.listing.world.Put(&watchv1.Row{Row: &watchv1.Row_Review{Review: &agentv1.WatchedReview{
			Id:           review.GetId(),
			AgentId:      "a1",
			RoundId:      review.GetRoundId(),
			RoundOrdinal: review.GetRoundOrdinal(),
			AuthorKind:   review.GetAuthorKind(),
			ReleasedAt:   review.GetReleasedAt(),
		}}})
	}

	for _, thread := range fake.threads {
		thread.AgentId = "a1"
		m.listing.world.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: thread}})
	}
}

func pressReviewBase(t *testing.T, m model) model {
	t.Helper()

	putServedRowsInWorld(&m)
	next, cmd := m.routeScreenMessage(keyMsg("<"))
	m = next.(model)
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	m.review.applyLoaded(&m, m.shared(), loaded)

	return m
}

func pressEdit(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.routeScreenMessage(keyMsg("e"))
	m = next.(model)
	require.Empty(t, m.review.notice)

	return settlePause(t, m, cmd)
}

func settlePause(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	require.NotNil(t, cmd)
	m.review.applyPaused(&m, m.shared(), findPaused(t, cmd()))

	return m
}

func findPaused(t *testing.T, msg tea.Msg) reviewPausedMsg {
	t.Helper()

	msgs := []tea.Msg{msg}
	if batch, ok := msg.(tea.BatchMsg); ok {
		msgs = nil
		for _, cmd := range batch {
			if cmd != nil {
				msgs = append(msgs, cmd())
			}
		}
	}

	for _, msg := range msgs {
		if paused, ok := msg.(reviewPausedMsg); ok {
			return paused
		}
	}

	require.FailNow(t, "the pass asked for nothing to stop")

	return reviewPausedMsg{}
}

func findRowWithAnAnchor(t *testing.T, m model) int {
	t.Helper()

	for row := range m.review.diff.view.Len() {
		if _, ok := m.review.diff.view.GetAnchor(row); ok {
			return row
		}
	}

	require.FailNow(t, "the diff draws no line a comment can hang on")

	return -1
}

func findCursorRow(t *testing.T, m model) string {
	t.Helper()

	for _, line := range strings.Split(m.render(), "\n") {
		if strings.Contains(ansi.Strip(line), "package greet") {
			return ansi.Cut(line, config.DefaultSidebarWidth, m.width)
		}
	}

	require.FailNow(t, "the diff draws no cursor row")

	return ""
}

func setReviewAgentState(m *model, state panel.State) {
	m.listing.agents[0].Agent.State = state
}

func typeReviewMessage(t *testing.T, m model, words string) model {
	t.Helper()

	for _, letter := range words {
		next, _ := m.routeScreenMessage(keyMsg(string(letter)))
		m = next.(model)
	}

	require.Equal(t, words, m.review.threads.editor.value())

	return m
}

func (m model) reviewHelp() keyMap {
	return m.review.help(m.shared())
}
