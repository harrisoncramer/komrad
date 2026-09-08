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
	// earlier is the round the person last released a review against, nil when there is none.
	earlier *agentv1.ReviewRound
	err     error
}

// reviewWroteMsg is one write and the review as it stands after it.
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
	return m, m.loadReviewCmd(loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     m.review.dir,
		Exclude: m.review.exclude,
	})
}

// stepReviewBase moves the diff between the point the branch left main and the round the person
// last reviewed.
func (m model) stepReviewBase() (tea.Model, tea.Cmd) {
	if m.review.base != "" {
		m.widenReviewBase()

		next, cmd := m.reloadReview()

		return next, cmd
	}

	m.review.base = m.review.baseRound.GetSnapshotSha()
	m.review.baseLabel = fmt.Sprintf("since round %d", m.review.baseRound.GetOrdinal())

	next, cmd := m.reloadReview()

	return next, cmd
}

// widenReviewBase puts the diff back to the whole change.
func (m *model) widenReviewBase() {
	m.review.base = ""
	m.review.baseLabel = ""
}

// reviewLegendFloor is the shortest pane the expanded legend fits in once it carries the base
// row.
const reviewLegendFloor = 21

// doesReviewFitBaseLegend reports whether the base key is worth a legend row.
func (m model) doesReviewFitBaseLegend() bool {
	// Explainer: the expanded legend is drawn whole rather than windowed, so a row past the pane
	// overruns it. The record gives way first at these heights and the newest legend row goes
	// next, while the key itself keeps working for a reader who knows it.
	if m.helpExpanded && m.height > 0 && m.height < reviewLegendFloor {
		return false
	}

	return m.review.baseRound != nil || m.review.base != ""
}

// reviewBaseDesc says which way the base key will move, since the same press narrows the diff and
// widens it again.
func (m model) reviewBaseDesc() string {
	if m.review.base != "" {
		return "diff from main"
	}

	return "diff since review"
}

// refuseReview backs out to the listing, for a worktree that turned out to hold nothing once it
// was read.
func (m *model) refuseReview(err error) {
	name := m.review.agentName
	m.review = reviewState{}

	m.enterScreen(screenAgents)
	m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
	m.stageSelection()
	m.reportBrief(describeRefusedReview(name, err), err.Error(), nil)
}

// describeRefusedReview says which of the two empty diffs this is: an agent that has written
// nothing, or one whose every line is in an excluded path.
func describeRefusedReview(name string, err error) string {
	if errors.Is(err, diff.ErrAllExcluded) {
		return "Everything " + name + " changed is excluded from the diff"
	}

	return "Nothing to review in " + name
}

// loadReviewCmdParams says which agent's round to open, where to read it, and what to measure it
// from.
type loadReviewCmdParams struct {
	AgentID string
	Dir     string
	Exclude []string
	// Base is the commit to measure the round from, empty for the point the branch left main.
	Base string
}

// loadReviewCmd opens a round of the agent and reads that rather than the worktree, and a tree
// that has not moved gives back the round already taken of it, so reopening the diff does not
// walk the ordinal on.
func (m model) loadReviewCmd(params loadReviewCmdParams) tea.Cmd {
	agents := m.api.agents
	agentID, dir, exclude := params.AgentID, params.Dir, params.Exclude

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

		base := round.GetBaseSha()
		if params.Base != "" {
			base = params.Base
		}

		patch, err := diff.Load(ctx, diff.LoadParams{
			Dir:         dir,
			BaseSHA:     base,
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
		earlier := previousReviewedRound(ctx, agents, round)

		return reviewLoadedMsg{
			agentID: agentID,
			round:   round,
			patch:   patch,
			threads: threads,
			viewed:  viewed,
			earlier: earlier,
			since: changedSinceLastReview(ctx, changedSinceLastReviewParams{
				Dir:         dir,
				Exclude:     exclude,
				Earlier:     earlier,
				SnapshotSHA: round.GetSnapshotSha(),
			}),
			err: err,
		}
	}
}

// changedSinceLastReviewParams says which round to measure against and what to leave out of the
// count.
type changedSinceLastReviewParams struct {
	Dir     string
	Exclude []string
	// Earlier is the round the person last reviewed, nil when there is none.
	Earlier *agentv1.ReviewRound
	// SnapshotSHA is the round on screen.
	SnapshotSHA string
}

// changedSinceLastReview says how much of this round is new since the person last let a review go,
// which is a courtesy rather than a fact the screen depends on, so anything that fails to answer
// says nothing.
func changedSinceLastReview(ctx context.Context, params changedSinceLastReviewParams) string {
	if params.Earlier == nil {
		return ""
	}

	changed, err := git.CountChangedFiles(ctx, git.CountChangedFilesParams{
		Dir:     params.Dir,
		FromSHA: params.Earlier.GetSnapshotSha(),
		ToSHA:   params.SnapshotSHA,
		Exclude: params.Exclude,
	})
	if err != nil || changed == 0 {
		return ""
	}

	return counted(changed, "file") + " changed since you last reviewed"
}

// previousReviewedRound is the round the person last released a review against, and nothing when
// they have not reviewed this agent before or when its base has moved since.
func previousReviewedRound(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	round *agentv1.ReviewRound,
) *agentv1.ReviewRound {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return nil
	}

	last := lastHumanReview(resp.Msg.GetReviews(), round.GetOrdinal())
	if last == nil {
		return nil
	}

	rounds, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return nil
	}

	for _, earlier := range rounds.Msg.GetRounds() {
		if earlier.GetId() != last.GetRoundId() {
			continue
		}

		// Explainer: two rounds measured from different points are not comparable. History was
		// rewritten under the branch between them, so a diff of the two snapshots would carry
		// whatever else moved on the base as though the agent had written it.
		if earlier.GetBaseSha() != round.GetBaseSha() {
			return nil
		}

		return earlier
	}

	return nil
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

	drewNothing := errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded)

	// A narrowed diff drawing nothing is an answer about the agent rather than a reason to leave,
	// so the whole change comes back with the answer said out loud.
	if drewNothing && m.review.base != "" {
		said := m.review.baseLabel
		m.widenReviewBase()
		m.review.notice = "nothing written " + said

		return m.loadReviewCmd(loadReviewCmdParams{
			AgentID: m.review.agentID,
			Dir:     m.review.dir,
			Exclude: m.review.exclude,
		})
	}

	// Both draw nothing, so both back out rather than leaving the reader in an empty viewer.
	if drewNothing {
		m.refuseReview(msg.err)

		return nil
	}

	m.review.loading = false
	m.review.err = msg.err
	if msg.patch == nil {
		return nil
	}

	m.review.baseRound = msg.earlier
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

// indexViewedFiles indexes the files read in this round; a later round starts with none.
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

	// Where the cursor is standing is taken before the threads are pinned again, which rebuilds
	// the rows a settled or deleted thread takes away from under it.
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

	return m, m.loadReviewCmd(loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     m.review.dir,
		Exclude: m.review.exclude,
		Base:    m.review.base,
	})
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
