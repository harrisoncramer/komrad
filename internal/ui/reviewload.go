package ui

import (
	"context"
	"errors"

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

// open puts the review on an agent's diff and reads it.
func (r *reviewScreen) open(s panelShared, agent panel.Agent) tea.Cmd {
	worktree := s.listing.worktreeFor(agent)

	*r = reviewScreen{
		agentID:   agent.ID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		exclude:   worktree.DiffExclude,
		loading:   true,
		// The whole change is what a review opens on, and it sits before the first round rather
		// than on it, so the first press of the step key lands on the newest round.
		diff:    reviewDiff{chapter: -1},
		threads: reviewThreads{seen: reviewProbeOf(s.listing.world, agent.ID)},
	}

	return r.loadWholeCmd(s)
}

// refuse backs out to the listing, for a worktree that turned out to hold nothing.
func (r *reviewScreen) refuse(h host, s panelShared, err error) tea.Cmd {
	name := r.agentName
	letGo := r.drop(h, s)
	h.reportBrief(describeRefusedReview(name, err), err.Error())

	return letGo
}

// abandon backs out to the listing for a diff that could not be read at all.
func (r *reviewScreen) abandon(h host, s panelShared, err error) tea.Cmd {
	name := r.agentName
	letGo := r.drop(h, s)
	h.report("Could not read the diff of "+name, err)

	return letGo
}

// drop puts the reader back on the row they opened the review from, letting go of an agent the
// pass stopped.
func (r *reviewScreen) drop(h host, s panelShared) tea.Cmd {
	letGo := r.endPass(h, s, abandonedSaid)

	*r = reviewScreen{}

	return tea.Batch(h.enter(screenAgents, screenTarget{}), letGo)
}

// describeRefusedReview says which of the two empty diffs this is: an agent that has written
// nothing, or one whose every line is in an excluded path.
func describeRefusedReview(name string, err error) string {
	if errors.Is(err, diff.ErrAllExcluded) {
		return "Everything " + name + " changed is excluded from the diff"
	}

	return "Nothing to review in " + name
}

// loadWholeCmd reads the whole change of the agent on screen.
func (r reviewScreen) loadWholeCmd(s panelShared) tea.Cmd {
	return loadReviewCmd(s, loadReviewCmdParams{
		AgentID: r.agentID,
		Dir:     r.dir,
		Exclude: r.exclude,
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
func loadReviewCmd(s panelShared, params loadReviewCmdParams) tea.Cmd {
	agents := s.api.agents
	agentID, dir, exclude := params.AgentID, params.Dir, params.Exclude
	repo := s.git

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
	Git     ReviewGit
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

func (r *reviewScreen) applyLoaded(h host, s panelShared, msg reviewLoadedMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	drewNothing := errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded)

	// A narrowed diff drawing nothing is an answer about the agent rather than a reason to leave,
	// so the whole change comes back with the answer said out loud.
	if drewNothing && r.diff.base != "" {
		said := r.diff.baseLabel
		r.diff.widen()
		r.notice = "nothing written " + said

		return r.loadWholeCmd(s)
	}

	// Both draw nothing, so both back out rather than leaving the reader in an empty viewer.
	if drewNothing {
		return r.refuse(h, s, msg.err)
	}

	if msg.patch == nil {
		return r.abandon(h, s, msg.err)
	}

	r.loading = false
	if msg.err != nil {
		h.report("Could not read the whole review of "+r.agentName, msg.err)
	}

	if r.diff.chapter >= len(msg.chapters) {
		r.diff.chapters = msg.chapters
		r.diff.widen()
		r.threads.cancelMessage()
		r.diff.drop()
		r.loading = true

		return r.loadWholeCmd(s)
	}

	r.root = msg.patch.Root
	r.round = msg.round
	r.diff.load(msg, s.isDark)
	r.threads.owed = msg.owed
	r.threads.set(msg.threads)

	r.scrollToCursor(s)

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

func (r *reviewScreen) applyWrote(h host, s panelShared, msg reviewWroteMsg) tea.Cmd {
	if msg.agentID != r.agentID || r.diff.view == nil {
		return nil
	}

	if msg.err != nil {
		// A send that did not land leaves the pass standing, so the reader keeps their comments
		// and keeps the agent stopped rather than being put back to watching with neither.
		h.report("Could not write on "+r.agentName, msg.err)

		return nil
	}

	r.notice = msg.notice

	if msg.readErr != nil {
		h.report("Could not read the review of "+r.agentName+" back", msg.readErr)
	} else {
		r.applyThreads(s, msg.threads)
	}

	if !msg.sent {
		return nil
	}

	// The comments are in the agent's hands now, and answering them is the next thing it does, so
	// this is what lets it go.
	return r.endPass(h, s, sentSaid)
}

// applyThreads draws the review as it now stands, keeping the reader where they were.
func (r *reviewScreen) applyThreads(s panelShared, threads []*agentv1.ReviewThread) {
	r.threads.set(threads)

	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  threads,
		Chapters: r.diff.chapters,
		At:       r.diff.chapter,
	})

	// Where the cursor is standing is taken before the threads are pinned again, which rebuilds
	// the rows a settled or deleted thread takes away from under it.
	place, ok := r.diff.view.GetPlace(r.diff.cursor)
	r.diff.view.PinThreads(drawn)
	r.diff.keepCursor(place, ok)

	r.scrollToCursor(s)
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

// reload reads the worktree again, holding the place the cursor is standing so the diff comes
// back under it.
func (r *reviewScreen) reload(s panelShared) tea.Cmd {
	if r.diff.view == nil {
		return nil
	}

	if place, ok := r.diff.view.GetPlace(r.diff.cursor); ok {
		r.diff.restore = &place
	}

	return loadReviewCmd(s, loadReviewCmdParams{
		AgentID: r.agentID,
		Dir:     r.dir,
		Exclude: r.exclude,
		Base:    r.diff.base,
		Head:    r.diff.head,
		Fresh:   r.pass.mode == modeReviewing,
	})
}

func (r *reviewScreen) leave(h host, s panelShared) tea.Cmd {
	if r.pass.atEditor {
		return r.closeEdit(h, s)
	}

	// The drop is the one way off this screen, and it puts the reader back on the row they came
	// from.
	return r.drop(h, s)
}

func (l listing) worktreeFor(agent panel.Agent) panel.WorktreeConfig {
	for _, workflow := range l.workflows {
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
