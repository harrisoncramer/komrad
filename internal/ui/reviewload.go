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
	// awaiting says the reader's last released review still holds comments nobody has settled, so
	// the panel will refuse a second one.
	awaiting bool
	// chapters are the rounds of the review as the reader can step through them, oldest first.
	chapters []reviewChapter
	// traced is where git says each thread's lines went, keyed by thread id, and empty for a
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
	// readErr is the review coming back unreadable after a write that landed, which is a stale
	// screen rather than a write to try again.
	readErr error
	// sent says this is the answer to a released review rather than to any of the other writes
	// this screen makes, which all come back the same way.
	sent bool
}

// enterReview opens the review of an agent's diff, with the threads in the sidebar and the
// diff in the stage.
func (m model) enterReview(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if !m.isReviewable(agent) {
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
		chapter: -1,
	}
	return m, m.loadReviewCmd(loadReviewCmdParams{
		AgentID: m.review.agentID,
		Dir:     m.review.dir,
		Exclude: m.review.exclude,
	})
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

// Explainer: the pass lives in this state and nowhere else, so an agent stopped for one is let go
// by the panel or not at all. Every way off this screen comes through here, including the ones
// nobody chose, such as a diff that stopped being readable part way through a pass.

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

		// Explainer: the reviews and the rounds answer six of the questions below between them,
		// and each question used to ask for them again. They are read once here and handed down,
		// which also keeps the six answers describing one moment rather than six.
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
		chapters := cutChapters(cutChaptersParams{
			Rounds:   rounds,
			Reviews:  reviews,
			OnScreen: round,
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
			awaiting:  isAwaitingReviewAnswer(reviews),
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
		return m.refuseReview(msg.err)
	}

	if msg.patch == nil {
		return m.abandonReview(msg.err)
	}

	m.review.loading = false
	if msg.err != nil {
		m.report("Could not read the whole review of "+m.review.agentName, msg.err)
	}

	if m.review.chapter >= len(msg.chapters) {
		m.review.chapters = msg.chapters
		m.widenReviewBase()
		*m = m.cancelReviewMessage()
		m.review.view = nil
		m.review.loading = true

		return m.loadReviewCmd(loadReviewCmdParams{
			AgentID: m.review.agentID,
			Dir:     m.review.dir,
			Exclude: m.review.exclude,
		})
	}

	m.review.baseRound = msg.earlier
	m.review.baseAt = msg.earlierAt
	m.review.pinned = msg.pinned
	m.review.awaiting = msg.awaiting
	m.review.chapters = msg.chapters
	m.review.root = msg.patch.Root
	m.review.view = review.NewView(msg.patch, m.isDark)
	m.review.view.SetTraced(msg.traced)
	m.review.view.SetViewed(indexViewedFiles(msg.viewed, msg.patch))
	m.review.view.PinThreads(threadsByChapter(threadsByChapterParams{
		Threads:  msg.threads,
		Chapters: m.review.chapters,
		At:       m.review.chapter,
	}))

	// The pattern lives on the ui model rather than the view, so a view built from nothing has
	// to be given it back, once the marks and the threads have settled the rows it walks.
	m.review.view.SetPattern(m.review.filter.pattern)

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

	return nil
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
	if msg.agentID != m.review.agentID || m.review.view == nil {
		return nil
	}

	if msg.err != nil {
		// A send that did not land leaves the pass standing, so the reader keeps their comments
		// and keeps the agent stopped rather than being put back to watching with neither.
		m.report("Could not write on "+m.review.agentName, msg.err)

		return nil
	}

	m.review.notice = msg.notice

	// Explainer: the write landing and the review being readable afterwards are two facts, and
	// only the first says what became of what the reader sent. Reading them as one turns a send
	// that landed into a send the screen believes never went, which then offers to send it again
	// and tells the agent nothing was sent on the way out.
	if msg.readErr != nil {
		m.report("Could not read the review of "+m.review.agentName+" back", msg.readErr)
	} else {
		m.applyReviewThreads(msg.threads)
	}

	// Explainer: every write on this screen comes back as one of these, so what says the pass is
	// over has to ride on the message rather than on the screen. A flag on the model says a send
	// is out and not which answer is its own, so a comment written just before it would end the
	// pass and let the agent go while the review was still in flight.
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

// keepReviewCursor puts the cursor back on what it was standing on, and failing that back inside
// the diff.
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
		Fresh:   m.review.mode == modeReviewing,
	})
}

func (m model) leaveReview() (tea.Model, tea.Cmd) {
	if m.review.atEditor {
		return m.closeReviewEdit()
	}

	// dropReview is the one way off this screen, since it is what lets go of an agent the pass
	// stopped, and it puts the reader back on the row they came from.
	return m, m.dropReview()
}

func (m model) worktreeFor(agent agentstore.Agent) workflowstore.WorktreeConfig {
	for _, workflow := range m.listing.workflows {
		if workflow.ID == agent.WorkflowID {
			return workflow.Worktree
		}
	}

	return workflowstore.WorktreeConfig{}
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
