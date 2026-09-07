package ui

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/diff"
	"weave/internal/git"
	"weave/internal/review"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

const reviewEditorHeight = 4

type reviewLoadedMsg struct {
	agentID string
	round   *agentv1.ReviewRound
	patch   *diff.Patch
	threads []*agentv1.ReviewThread
	viewed  []*agentv1.ReviewViewedFile
	// since says how much of this round is new to the reader, which is a courtesy rather than
	// something the screen depends on.
	since string
	err   error
}

// reviewWroteMsg is one write and the review as it stands after it, since the panel keeps no
// copy of a thread it has just changed and reads the review back instead.
type reviewWroteMsg struct {
	agentID string
	notice  string
	threads []*agentv1.ReviewThread
	err     error
}

// enterReview opens the review of an agent's diff, with the threads in the sidebar and the
// diff in the stage.
func (m model) enterReview(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if !m.isReviewable(agent) {
		return m, nil
	}

	worktree := m.worktreeFor(agent)

	editor := textarea.New()
	editor.Placeholder = "what should the agent change here?"
	editor.ShowLineNumbers = false
	editor.SetHeight(reviewEditorHeight)

	m.enterScreen(screenReview)
	m.review = reviewState{
		agentID:   agent.ID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		exclude:   worktree.DiffExclude,
		editor:    editor,
		loading:   true,
	}
	// The review draws the diff and the sidebar itself, so it takes the window off the panes it
	// would otherwise be sharing with.
	m.frame.Solo(true)

	return m, m.loadReviewCmd(m.review.agentID, m.review.dir, m.review.exclude)
}

// refuseReview backs out to the listing, for a worktree that turned out to hold nothing once it
// was read, since the screen the reader is dropped onto is the one the banner belongs to.
func (m *model) refuseReview(err error) {
	name := m.review.agentName
	m.review = reviewState{}

	m.enterScreen(screenAgents)
	m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
	m.stageSelection()
	m.reportBrief(describeRefusedReview(name, err), err.Error(), nil)
}

// describeRefusedReview says which of the two empty diffs this is, since an agent that has written
// nothing and an agent whose every line is in an excluded path want opposite things said about
// them: one has not worked yet, the other has and you asked not to be shown it.
func describeRefusedReview(name string, err error) string {
	if errors.Is(err, diff.ErrAllExcluded) {
		return "Everything " + name + " changed is excluded from the diff"
	}

	return "Nothing to review in " + name
}

// loadReviewCmd opens a round of the agent and reads that rather than the worktree, and a tree
// that has not moved gives back the round already taken of it, so reopening the diff does not
// walk the ordinal on.
func (m model) loadReviewCmd(agentID, dir string, exclude []string) tea.Cmd {
	agents := m.api.agents

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		opened, err := agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
			AgentId: agentID,
		}))
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		round := opened.Msg.GetRound()

		patch, err := diff.Load(ctx, diff.LoadParams{
			Dir:         dir,
			BaseSHA:     round.GetBaseSha(),
			SnapshotSHA: round.GetSnapshotSha(),
			Exclude:     exclude,
		})
		if err != nil {
			return reviewLoadedMsg{
				agentID: agentID,
				round:   round,
				err:     err,
			}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)
		if err != nil {
			return reviewLoadedMsg{
				agentID: agentID,
				round:   round,
				patch:   patch,
				err:     err,
			}
		}

		viewed, err := listReviewViewedFiles(ctx, agents, round.GetId())

		return reviewLoadedMsg{
			agentID: agentID,
			round:   round,
			patch:   patch,
			threads: threads,
			viewed:  viewed,
			since:   changedSinceLastReview(ctx, agents, dir, exclude, round),
			err:     err,
		}
	}
}

// changedSinceLastReview says how much of this round is new since the person last let a review go,
// which is a courtesy rather than a fact the screen depends on, so anything that fails to answer
// says nothing.
func changedSinceLastReview(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	dir string,
	exclude []string,
	round *agentv1.ReviewRound,
) string {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return ""
	}

	last := lastHumanReview(resp.Msg.GetReviews(), round.GetOrdinal())
	if last == nil {
		return ""
	}

	rounds, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return ""
	}

	from := ""
	for _, earlier := range rounds.Msg.GetRounds() {
		if earlier.GetId() == last.GetRoundId() {
			from = earlier.GetSnapshotSha()
		}
	}

	changed, err := git.CountChangedFiles(ctx, git.CountChangedFilesParams{
		Dir:     dir,
		FromSHA: from,
		ToSHA:   round.GetSnapshotSha(),
		Exclude: exclude,
	})
	if err != nil || changed == 0 {
		return ""
	}

	return counted(changed, "file") + " changed since you last reviewed"
}

// lastHumanReview is the newest review the person let go of against a round earlier than this
// one, which is where they last looked.
func lastHumanReview(reviews []*agentv1.Review, ordinal int32) *agentv1.Review {
	var last *agentv1.Review
	for _, review := range reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() == "" || review.GetRoundOrdinal() >= ordinal {
			continue
		}

		last = review
	}

	return last
}

func listReviewViewedFiles(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	roundID string,
) ([]*agentv1.ReviewViewedFile, error) {
	resp, err := agents.ListReviewViewedFiles(ctx, connect.NewRequest(&agentv1.ListReviewViewedFilesRequest{
		RoundId: roundID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read which files you have already read: %w", err)
	}

	return resp.Msg.GetViewed(), nil
}

func listReviewThreads(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) ([]*agentv1.ReviewThread, error) {
	resp, err := agents.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the review of %q: %w", agentID, err)
	}

	return resp.Msg.GetThreads(), nil
}

func (m *model) applyReviewLoaded(msg reviewLoadedMsg) tea.Cmd {
	if msg.agentID != m.review.agentID {
		return nil
	}

	// Both draw nothing, so both back out rather than leaving the reader in an empty viewer.
	if errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded) {
		m.refuseReview(msg.err)

		return nil
	}

	m.review.loading = false
	m.review.err = msg.err
	if msg.patch == nil {
		return nil
	}

	m.review.root = msg.patch.Root
	m.review.view = review.NewView(msg.patch, m.isDark)
	m.review.view.SetViewed(indexViewedFiles(msg.viewed))
	m.review.view.PinThreads(msg.threads)

	// The pattern lives on the ui model rather than the view, so a view built from nothing has
	// to be given it back, once the marks and the threads have settled the rows it walks.
	m.review.view.SetPattern(m.review.filter.pattern)

	// Explainer: A new round is a different snapshot, so an edit held against the last one no
	// longer names lines of what is on screen. The checkout itself is kept, since it is named
	// after the agent and is moved onto the new snapshot the next time the editor asks for it.
	if m.review.round.GetSnapshotSha() != msg.round.GetSnapshotSha() {
		// A checkout still standing on the round being left holds whatever was saved into it, and
		// the comments already written from it belong to that round rather than to this one.
		if m.review.roundDir != "" && m.review.roundSha != msg.round.GetSnapshotSha() {
			m.review.notice = "the round moved on, so your comments stay on the one you wrote them against"
		}

		m.review.oversized = false
	}

	m.setReviewThreads(msg.threads)
	m.review.round = msg.round
	m.review.moved = false
	m.review.since = msg.since
	m.review.cursor = m.review.view.GetFirstRow()
	if place := m.review.restore; place != nil {
		if found, ok := m.review.view.Locate(*place); ok {
			m.review.cursor = found
		}

		m.review.restore = nil
	}

	m.scrollReviewToCursor()

	// A checkout weave already owns is the one on screen, so only a screen opening on an agent it
	// holds nothing for has an earlier sitting's work to look for.
	if m.review.roundDir != "" {
		return nil
	}

	return m.findRoundLeftoverCmd()
}

// indexViewedFiles indexes the files read in this round; a later round starts with none because
// marks belong to one round and need no comparison.
func indexViewedFiles(viewed []*agentv1.ReviewViewedFile) map[string]bool {
	held := map[string]bool{}
	for _, mark := range viewed {
		held[mark.GetFilePath()] = true
	}

	return held
}

func (m *model) applyReviewWrote(msg reviewWroteMsg) {
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return
	}

	m.review.err = msg.err
	if msg.err != nil {
		return
	}

	m.review.notice = msg.notice
	m.applyReviewThreads(msg.threads)
}

// applyReviewThreads draws the review as it now stands, keeping the reader where they were.
func (m *model) applyReviewThreads(threads []*agentv1.ReviewThread) {
	m.setReviewThreads(threads)

	// Where the cursor is standing is taken before the threads are pinned again, since pinning
	// rebuilds the rows the threads with nowhere to stand are drawn on, and a thread settled or
	// deleted takes one of those rows away from under the cursor.
	place, ok := m.review.view.GetPlace(m.review.cursor)
	m.review.view.PinThreads(threads)
	m.keepReviewCursor(place, ok)

	m.scrollReviewToCursor()
}

// keepReviewCursor puts the cursor back on what it was standing on, and failing that, back
// inside the diff.
func (m *model) keepReviewCursor(place review.Place, ok bool) {
	was := m.review.cursor

	m.review.cursor = min(m.review.cursor, m.review.view.GetLastRow())
	if ok {
		if found, located := m.review.view.Locate(place); located {
			m.review.cursor = found
		}
	}

	if m.review.cursor != was {
		m.review.pinTop = false
	}
}

// reloadReview reads the worktree again, holding the place the cursor is standing so the diff
// comes back under it.
func (m model) reloadReview() (model, tea.Cmd) {
	if m.review.view == nil {
		return m, nil
	}

	if place, ok := m.review.view.GetPlace(m.review.cursor); ok {
		m.review.restore = &place
	}

	return m, m.loadReviewCmd(m.review.agentID, m.review.dir, m.review.exclude)
}

func (m model) leaveReview() (tea.Model, tea.Cmd) {
	reap := m.reapRoundCmd(m.review.root, m.review.roundDir)
	m.review = reviewState{}

	next, cmd := m.returnToAgents()

	return next, tea.Batch(cmd, reap)
}

func (m model) worktreeFor(agent agentstore.Agent) workflowstore.WorktreeConfig {
	for _, workflow := range m.listing.workflows {
		if workflow.ID == agent.WorkflowID {
			return workflow.Worktree
		}
	}

	return workflowstore.WorktreeConfig{}
}
