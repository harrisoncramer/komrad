package ui

import (
	"context"
	"errors"
	"slices"

	"weave/internal/diff"
	"weave/internal/git"
	"weave/internal/ui/diffview"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

const reviewEditorHeight = 4

// hasCheckout reports whether an agent's directory is still there to read.
func hasCheckout(agent panel.Agent) bool {
	return agent.Cwd != "" && !agent.WorktreeRemoved
}

// isReviewable reports whether an agent has work of its own to read.
func isReviewable(agent panel.Agent) bool {
	return hasCheckout(agent) && agent.ParentAgentID == nil
}

// isRebaseable reports whether an agent sits in a worktree weave cut for it alone.
func (m model) isRebaseable(agent panel.Agent) bool {
	if !hasCheckout(agent) {
		return false
	}

	index := slices.IndexFunc(m.listing.workflows, func(w panel.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	// A child sits in its parent's checkout.
	if agent.ParentAgentID != nil {
		return false
	}

	return m.listing.workflows[index].Worktree.Enabled
}

type reviewLoadedMsg struct {
	agentID string
	round   *agentv1.ReviewRound
	patch   *diff.Patch
	threads []*agentv1.ReviewThread
	viewed  []*agentv1.ReviewViewedFile
	// since says how much of this round is new to the reader.
	since string
	// earlier is the round the person last released a review against, nil when there is none.
	earlier *agentv1.ReviewRound
	// earlierAt is when they released it, in clock terms, empty when there is none.
	earlierAt string
	// pinned says the diff is the snapshot the reader's unsent comments were written against
	// rather than the work as it now stands.
	pinned bool
	// owed is the id of the reader's last released review while it still holds comments nobody
	// has settled, empty once a second review would be allowed.
	owed string
	// chapters are the rounds of the review as the reader can step through them, oldest first.
	chapters []reviewChapter
	// traced is where git says each thread's lines went, keyed by thread id, and empty for a
	// repository that could not answer.
	traced map[string]diffview.Traced
	err    error
}

// reviewWroteMsg is one write and the review as it stands after it.
type reviewWroteMsg struct {
	agentID string
	notice  string
	threads []*agentv1.ReviewThread
	err     error
	// readErr is the review coming back unreadable after a write that landed.
	readErr error
	// sent says this is the answer to a released review rather than to any of the other writes
	// this screen makes, which all come back the same way.
	sent bool
}

// enterReview opens the review of an agent's diff, with the threads in the sidebar and the
// diff in the stage.
func (m model) enterReview(agent panel.Agent) (tea.Model, tea.Cmd) {
	if !isReviewable(agent) {
		return m, nil
	}

	worktree := m.worktreeFor(agent)

	m.enterScreen(screenReview)
	m.review = reviewState{
		agentID:   agent.ID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		exclude:   worktree.DiffExclude,
		loading:   true,
		// The whole change is what a review opens on, and it sits before the first round rather
		// than on it, so the first press of the step key lands on the newest round.
		diff: reviewDiff{chapter: -1},
	}

	return m, m.loadWholeReviewCmd()
}

// refuseReview backs out to the listing, for a worktree that turned out to hold nothing.
func (m *model) refuseReview(err error) tea.Cmd {
	name := m.review.agentName
	letGo := m.dropReview()
	m.reportBrief(describeRefusedReview(name, err), err.Error(), nil)

	return letGo
}

// abandonReview backs out to the listing for a diff that could not be read at all.
func (m *model) abandonReview(err error) tea.Cmd {
	name := m.review.agentName
	letGo := m.dropReview()
	m.report("Could not read the diff of "+name, err)

	return letGo
}

// dropReview puts the reader back on the row they opened the review from, letting go of an agent
// the pass stopped.
func (m *model) dropReview() tea.Cmd {
	next, letGo := m.endReviewPass(abandonedSaid)
	if ended, ok := next.(model); ok {
		*m = ended
	}

	m.review = reviewState{}

	m.enterScreen(screenAgents)
	m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
	m.stageSelection()

	return letGo
}

// describeRefusedReview says which of the two empty diffs this is: an agent that has written
// nothing, or one whose every line is in an excluded path.
func describeRefusedReview(name string, err error) string {
	if errors.Is(err, diff.ErrAllExcluded) {
		return "Everything " + name + " changed is excluded from the diff"
	}

	return "Nothing to review in " + name
}

// loadWholeReviewCmd reads the whole change of the agent on screen.
func (m model) loadWholeReviewCmd() tea.Cmd {
	return m.loadReviewCmd(loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     m.review.dir,
		Exclude: m.review.exclude,
	})
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
	Head  string
	Fresh bool
}

// loadReviewCmd opens a round of the agent and reads that rather than the worktree, a tree that
// has not moved giving back the round already taken of it.
func (m model) loadReviewCmd(params loadReviewCmdParams) tea.Cmd {
	agents := m.api.agents
	agentID, dir, exclude := params.AgentID, params.Dir, params.Exclude
	repo := m.git

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		threads, err := listReviewThreads(ctx, agents, agentID)
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		reviews, err := listReviews(ctx, agents, agentID)
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		// The rounds are a better answer where they can be had rather than one the screen depends
		// on, so only the pin, which cannot be placed without them, gives up when they cannot.
		rounds, roundsErr := listReviewRounds(ctx, agents, agentID)

		openBatch := openBatchRoundID(reviews)
		if openBatch != "" && roundsErr != nil {
			return reviewLoadedMsg{agentID: agentID, err: roundsErr}
		}

		var round *agentv1.ReviewRound

		var pinned bool
		if !params.Fresh {
			round, pinned = findPendingRound(rounds, openBatch)
		}

		if round == nil {
			opened, err := agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
				AgentId: agentID,
			}))
			if err != nil {
				return reviewLoadedMsg{agentID: agentID, err: err}
			}

			round = opened.Msg.GetRound()
			rounds = withRound(rounds, round)
		}

		base := round.GetBaseSha()
		if params.Base != "" {
			base = params.Base
		}

		head := round.GetSnapshotSha()
		if params.Head != "" {
			head = params.Head
		}

		patch, err := diff.Load(ctx, repo, diff.LoadParams{
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

		viewed, err := listReviewViewedFiles(ctx, agents, agentID)
		earlier := findPreviousReviewedRound(reviews, rounds, round)
		chapters := replayChapters(ctx, replayChaptersParams{
			Git: repo,
			Dir: dir,
			Chapters: cutChapters(cutChaptersParams{
				Rounds:   rounds,
				Reviews:  reviews,
				OnScreen: round,
			}),
		})
		traced := traceThreads(ctx, traceThreadsParams{
			Git:     repo,
			Dir:     dir,
			ToSHA:   head,
			Threads: threads,
			Rounds:  rounds,
		})

		return reviewLoadedMsg{
			agentID:   agentID,
			round:     round,
			patch:     patch,
			threads:   threads,
			viewed:    viewed,
			earlier:   earlier,
			earlierAt: releasedAtOf(reviews, round),
			pinned:    pinned,
			owed:      findLastOwedReview(reviews).GetId(),
			chapters:  chapters,
			traced:    traced,
			since: changedSinceLastReview(ctx, changedSinceLastReviewParams{
				Git:         repo,
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
	Git     uiGit
	Dir     string
	Exclude []string
	// Earlier is the round the person last reviewed, nil when there is none.
	Earlier *agentv1.ReviewRound
	// SnapshotSHA is the round on screen.
	SnapshotSHA string
}

// changedSinceLastReview says how much of this round is new since the person last released a
// review, saying nothing at all when it cannot answer.
func changedSinceLastReview(ctx context.Context, params changedSinceLastReviewParams) string {
	if params.Earlier == nil {
		return ""
	}

	changed, err := params.Git.CountChangedFiles(ctx, git.CountChangedFilesParams{
		Dir:     params.Dir,
		FromSHA: params.Earlier.GetSnapshotSha(),
		ToSHA:   params.SnapshotSHA,
		Exclude: params.Exclude,
	})
	if err != nil || changed == 0 {
		return ""
	}

	return label.Counted(changed, "file") + " changed since you last reviewed"
}

func (m *model) applyReviewLoaded(msg reviewLoadedMsg) tea.Cmd {
	if msg.agentID != m.review.agentID {
		return nil
	}

	drewNothing := errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded)

	// A narrowed diff drawing nothing is an answer about the agent rather than a reason to leave,
	// so the whole change comes back with the answer said out loud.
	if drewNothing && m.review.diff.base != "" {
		said := m.review.diff.baseLabel
		m.review.diff.widen()
		m.review.notice = "nothing written " + said

		return m.loadWholeReviewCmd()
	}

	// Both draw nothing, so both back out rather than leaving the reader in an empty viewer.
	if drewNothing {
		return m.refuseReview(msg.err)
	}

	if msg.patch == nil {
		return m.abandonReview(msg.err)
	}

	m.review.loading = false
	if msg.err != nil {
		m.report("Could not read the whole review of "+m.review.agentName, msg.err)
	}

	if m.review.diff.chapter >= len(msg.chapters) {
		m.review.diff.chapters = msg.chapters
		m.review.diff.widen()
		m.review.threads.cancelMessage()
		m.review.diff.drop()
		m.review.loading = true

		return m.loadWholeReviewCmd()
	}

	m.review.root = msg.patch.Root
	m.review.round = msg.round
	m.review.diff.load(msg, m.isDark)
	m.review.threads.owed = msg.owed
	m.review.threads.set(msg.threads)

	m.scrollReviewToCursor()

	return nil
}

// drop puts the patch down until the next one arrives, keeping the files drawn whole for it.
func (d *reviewDiff) drop() {
	if d.view != nil {
		d.expanded = d.view.GetExpanded()
	}

	d.view = nil
}

// load draws a patch just read, over the one on screen, and puts the cursor back where the
// reader was standing.
func (d *reviewDiff) load(msg reviewLoadedMsg, isDark bool) {
	d.baseRound = msg.earlier
	d.baseAt = msg.earlierAt
	d.pinned = msg.pinned
	d.chapters = msg.chapters
	d.moved = false
	d.since = msg.since

	// The files the reader opened out are the view's own, so a view built again over the patch
	// takes them from the one it replaces, or from what was kept when that one was dropped.
	view := diffview.NewView(msg.patch, isDark)
	view.SetNarrowed(d.base != "")
	view.SetTraced(msg.traced)
	view.SetViewed(indexViewedFiles(msg.viewed, msg.patch))
	if d.view != nil {
		d.expanded = d.view.GetExpanded()
	}

	view.SetExpanded(d.expanded)
	d.expanded = nil

	view.PinThreads(threadsByChapter(threadsByChapterParams{
		Threads:  msg.threads,
		Chapters: d.chapters,
		At:       d.chapter,
	}))

	// The pattern is the screen's rather than the view's, so a view built from nothing is given
	// it back once the marks and the threads have settled the rows it walks.
	view.SetPattern(d.filter.pattern)
	d.view = view

	d.cursor = view.GetFirstRow()
	if place := d.restore; place != nil {
		if found, ok := view.Locate(*place); ok {
			d.cursor = found
		}

		d.restore = nil
	}
}

// indexViewedFiles indexes the files whose stored fingerprint still matches the current patch,
// so a file the agent has rewritten since it was read shows as unread again.
func indexViewedFiles(viewed []*agentv1.ReviewViewedFile, patch *diff.Patch) map[string]bool {
	fingerprints := map[string]string{}
	for i := range patch.Files {
		fingerprints[patch.Files[i].Path()] = patch.Files[i].Fingerprint()
	}

	held := map[string]bool{}
	for _, mark := range viewed {
		if fp, ok := fingerprints[mark.GetFilePath()]; ok && fp == mark.GetFingerprint() {
			held[mark.GetFilePath()] = true
		}
	}

	return held
}

func (m *model) applyReviewWrote(msg reviewWroteMsg) tea.Cmd {
	if msg.agentID != m.review.agentID || m.review.diff.view == nil {
		return nil
	}

	if msg.err != nil {
		// A send that did not land leaves the pass standing, so the reader keeps their comments
		// and keeps the agent stopped rather than being put back to watching with neither.
		m.report("Could not write on "+m.review.agentName, msg.err)

		return nil
	}

	m.review.notice = msg.notice

	if msg.readErr != nil {
		m.report("Could not read the review of "+m.review.agentName+" back", msg.readErr)
	} else {
		m.applyReviewThreads(msg.threads)
	}

	if !msg.sent {
		return nil
	}

	// The comments are in the agent's hands now, and answering them is the next thing it does, so
	// this is what lets it go.
	next, letGo := m.endReviewPass(sentSaid)
	if ended, ok := next.(model); ok {
		*m = ended
	}

	return letGo
}

// applyReviewThreads draws the review as it now stands, keeping the reader where they were.
func (m *model) applyReviewThreads(threads []*agentv1.ReviewThread) {
	m.review.threads.set(threads)

	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  threads,
		Chapters: m.review.diff.chapters,
		At:       m.review.diff.chapter,
	})

	// Where the cursor is standing is taken before the threads are pinned again, which rebuilds
	// the rows a settled or deleted thread takes away from under it.
	place, ok := m.review.diff.view.GetPlace(m.review.diff.cursor)
	m.review.diff.view.PinThreads(drawn)
	m.review.diff.keepCursor(place, ok)

	m.scrollReviewToCursor()
}

// keepCursor puts the cursor back on what it was standing on, and failing that back inside the
// diff.
func (d *reviewDiff) keepCursor(place diffview.Place, ok bool) {
	was := d.cursor

	d.cursor = min(d.cursor, d.view.GetLastRow())
	if ok {
		if found, located := d.view.Locate(place); located {
			d.cursor = found
		}
	}

	if d.cursor != was {
		d.pinTop = false
	}
}

// reloadReview reads the worktree again, holding the place the cursor is standing so the diff
// comes back under it.
func (m model) reloadReview() (model, tea.Cmd) {
	if m.review.diff.view == nil {
		return m, nil
	}

	if place, ok := m.review.diff.view.GetPlace(m.review.diff.cursor); ok {
		m.review.diff.restore = &place
	}

	return m, m.loadReviewCmd(loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     m.review.dir,
		Exclude: m.review.exclude,
		Base:    m.review.diff.base,
		Head:    m.review.diff.head,
		Fresh:   m.review.pass.mode == modeReviewing,
	})
}

func (m model) leaveReview() (tea.Model, tea.Cmd) {
	if m.review.pass.atEditor {
		return m.closeReviewEdit()
	}

	// dropReview is the one way off this screen, and it puts the reader back on the row they
	// came from.
	return m, m.dropReview()
}

func (m model) worktreeFor(agent panel.Agent) panel.WorktreeConfig {
	for _, workflow := range m.listing.workflows {
		if workflow.ID == agent.WorkflowID {
			return workflow.Worktree
		}
	}

	return panel.WorktreeConfig{}
}

// withRound adds a round just opened to the list read before it was.
func withRound(rounds []*agentv1.ReviewRound, opened *agentv1.ReviewRound) []*agentv1.ReviewRound {
	for _, held := range rounds {
		if held.GetId() == opened.GetId() {
			return rounds
		}
	}

	return append(rounds, opened)
}
