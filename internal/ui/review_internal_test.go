package ui

import (
	"context"
	"fmt"
	"testing"

	"weave/internal/gittest"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	watchv1 "weave/proto/gen/watch/v1"

	"connectrpc.com/connect"
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

func (m model) reviewHelp() keyMap {
	return m.review.help(m.shared())
}
