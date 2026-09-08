package ui

import (
	"context"
	"errors"

	"weave/internal/diff"
	"weave/internal/git"
	"weave/internal/review"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"

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
	// earlierAt is when they released it, in clock terms, empty when there is none.
	earlierAt string
	// pinned says the diff is the snapshot the reader's unsent comments were written against
	// rather than the work as it now stands.
	pinned bool
	// awaiting says the reader's last released review has produced no work yet, so the panel will
	// refuse a second one.
	awaiting bool
	// chapters are the rounds of the review as the reader can step through them, oldest first.
	chapters []reviewChapter
	// traced is where git says each thread's lines went, keyed by thread id, empty for a
	// repository that could not answer.
	traced map[string]review.Traced
	err    error
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
		// The whole change is what a review opens on, and it sits before the first round rather
		// than on it, so the first press of the step key lands on the newest round.
		chapter: -1,
	}
	return m, m.loadReviewCmd(loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     m.review.dir,
		Exclude: m.review.exclude,
	})
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
	// Head is the commit to measure it to, empty for the round's own snapshot.
	Head string
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

		threads, err := listReviewThreads(ctx, agents, agentID)
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		round, pinned, err := pendingRound(ctx, agents, pendingRoundParams{AgentID: agentID})
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		if round == nil {
			opened, err := agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
				AgentId: agentID,
			}))
			if err != nil {
				return reviewLoadedMsg{agentID: agentID, err: err}
			}

			round = opened.Msg.GetRound()
		}

		base := round.GetBaseSha()
		if params.Base != "" {
			base = params.Base
		}

		head := round.GetSnapshotSha()
		if params.Head != "" {
			head = params.Head
		}

		patch, err := diff.Load(ctx, diff.LoadParams{
			Dir:         dir,
			BaseSHA:     base,
			SnapshotSHA: head,
			Exclude:     exclude,
		})
		if err != nil {
			return reviewLoadedMsg{
				agentID: agentID,
				round:   round,
				err:     err,
			}
		}

		viewed, err := listReviewViewedFiles(ctx, agents, round.GetId())
		earlier := previousReviewedRound(ctx, agents, round)
		awaiting := awaitingReviewAnswer(ctx, agents, round)
		chapters := reviewChapters(ctx, agents, round)
		earlierAt := releasedAtOf(ctx, agents, round)
		traced := traceThreads(ctx, traceThreadsParams{
			Dir:     dir,
			ToSHA:   head,
			Threads: threads,
			Rounds:  roundsOf(ctx, agents, agentID),
		})

		return reviewLoadedMsg{
			agentID:   agentID,
			round:     round,
			patch:     patch,
			threads:   threads,
			viewed:    viewed,
			earlier:   earlier,
			earlierAt: earlierAt,
			pinned:    pinned,
			awaiting:  awaiting,
			chapters:  chapters,
			traced:    traced,
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
	m.review.baseAt = msg.earlierAt
	m.review.pinned = msg.pinned
	m.review.awaiting = msg.awaiting
	m.applyReviewChapters(msg.chapters)
	m.review.root = msg.patch.Root
	m.review.view = review.NewView(msg.patch, m.isDark)
	m.review.view.SetTraced(msg.traced)
	m.review.view.SetViewed(indexViewedFiles(msg.viewed))
	m.review.view.PinThreads(threadsByChapter(threadsByChapterParams{
		Threads:  msg.threads,
		Chapters: m.review.chapters,
		At:       m.review.chapter,
	}))

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
		m.report("Could not write on "+m.review.agentName, msg.err)

		return
	}

	m.review.notice = msg.notice
	m.applyReviewThreads(msg.threads)
}

// applyReviewThreads draws the review as it now stands, keeping the reader where they were.
func (m *model) applyReviewThreads(threads []*agentv1.ReviewThread) {
	m.setReviewThreads(threads)

	// Explainer: a round is a moment, so a comment written after it is not something the reader
	// could have said about that code. The whole conversation stays in the counts above, since
	// that is the reader's own standing state, and only what is drawn against the diff is cut
	// back to the comments that existed by the end of the round on screen.
	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  threads,
		Chapters: m.review.chapters,
		At:       m.review.chapter,
	})

	// Where the cursor is standing is taken before the threads are pinned again, which rebuilds
	// the rows a settled or deleted thread takes away from under it.
	place, ok := m.review.view.GetPlace(m.review.cursor)
	m.review.view.PinThreads(drawn)
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
		Head:    m.review.head,
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
