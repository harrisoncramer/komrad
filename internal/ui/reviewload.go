package ui

import (
	"context"
	"errors"

	"komrad/internal/assemble"
	"komrad/internal/diff"
	"komrad/internal/git"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
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
	// chapters are the rounds of the review as the reader can step through them, oldest first.
	chapters []reviewChapter
	rounds   []*agentv1.ReviewRound
	// replayed says the older side was carried across a rebase onto the newer one's base.
	replayed bool
	// answerBase and answerHead are the snapshots an answer on screen was measured between.
	answerBase string
	answerHead string
	// notice is what the load has to say about the round it settled on.
	notice string
	// traced is where git says each thread's lines went, keyed by thread id, and empty for a
	// repository that could not answer.
	traced map[string]diffview.Traced
	err    error
}

// reviewWroteMsg is one write on the review.
type reviewWroteMsg struct {
	agentID string
	notice  string
	err     error
	// readBack says threads is the review of an archived agent read back after the write.
	readBack bool
	threads  []*agentv1.ReviewThread
	readErr  error
	// unsent is the message a refused write was carrying, nil for any other write.
	unsent *reviewUnsent
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
		diff: reviewDiff{chapter: -1},
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

// drop puts the reader back on the row they opened the review from, letting go of an agent
// stopped for the editor.
func (r *reviewScreen) drop(h host, s panelShared) tea.Cmd {
	letGo := r.endHold(h, s, abandonedSaid)

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
	Head string
	// Answer is the answer to measure between, found among the rounds the load lists.
	Answer *reviewAnswer
	Fresh  bool
}

// loadReviewCmd opens a round of the agent and reads that rather than the worktree, a tree that
// has not moved giving back the round already taken of it.
func loadReviewCmd(s panelShared, params loadReviewCmdParams) tea.Cmd {
	agents := s.api.agents
	agentID, dir, exclude := params.AgentID, params.Dir, params.Exclude
	repo := s.git
	_, live := s.listing.liveAgent(agentID)
	held := assemble.ReviewThreads(s.listing.world, agentID)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		threads, err := readThreads(ctx, readThreadsParams{
			Agents:  agents,
			AgentID: agentID,
			Live:    live,
			Held:    held,
		})
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

		var fromBase, toBase, answerBase, answerHead string
		if answer := params.Answer; answer != nil {
			from, to := findRound(rounds, answer.FromRound), findRound(rounds, answer.ToRound)
			if from == nil || to == nil {
				return reviewLoadedMsg{agentID: agentID, err: errAnswerGone}
			}

			answerBase, answerHead = from.GetSnapshotSha(), to.GetSnapshotSha()
			params.Base, params.Head = answerBase, answerHead
			fromBase, toBase = from.GetBaseSha(), to.GetBaseSha()
		}

		var round *agentv1.ReviewRound

		var pinned bool
		if !params.Fresh {
			round, pinned = findPendingRound(rounds, openBatch)
		}

		var notice string
		if round == nil {
			opened, err := agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
				AgentId: agentID,
			}))
			switch {
			case connect.CodeOf(err) == connect.CodeFailedPrecondition && len(rounds) > 0:
				round = rounds[len(rounds)-1]
				notice = "a rebase or merge is part way through, so this is the work as it stood before it"
			case err != nil:
				return reviewLoadedMsg{agentID: agentID, err: err}
			default:
				round = opened.Msg.GetRound()
				rounds = withRound(rounds, round)
			}
		}

		base := round.GetBaseSha()
		if params.Base != "" {
			base = params.Base
		}

		head := round.GetSnapshotSha()
		if params.Head != "" {
			head = params.Head
		}

		compared := compareRounds(ctx, compareRoundsParams{
			Git:      repo,
			Dir:      dir,
			FromSHA:  base,
			FromBase: fromBase,
			ToBase:   toBase,
		})
		if !compared.Comparable {
			return reviewLoadedMsg{
				agentID: agentID,
				round:   round,
				err:     &roundsApartError{Conflicts: compared.Conflicts},
			}
		}

		base = compared.FromSHA

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
			agentID:    agentID,
			round:      round,
			patch:      patch,
			threads:    threads,
			viewed:     viewed,
			earlier:    earlier,
			earlierAt:  releasedAtOf(reviews, round),
			pinned:     pinned,
			chapters:   chapters,
			rounds:     rounds,
			replayed:   compared.Replayed,
			traced:     traced,
			answerBase: answerBase,
			answerHead: answerHead,
			notice:     notice,
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

// readThreadsParams says whose conversation to read, and what the world already holds of it.
type readThreadsParams struct {
	Agents  agentv1connect.AgentServiceClient
	AgentID string
	// Live says the agent's threads are the world's.
	Live bool
	Held []*agentv1.ReviewThread
}

// readThreads is the conversation on an agent's diff, from the world unless the agent is archived.
func readThreads(ctx context.Context, params readThreadsParams) ([]*agentv1.ReviewThread, error) {
	if params.Live {
		return params.Held, nil
	}

	return listReviewThreads(ctx, params.Agents, params.AgentID)
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

	var apart *roundsApartError
	if errors.As(msg.err, &apart) || errors.Is(msg.err, errAnswerGone) {
		r.diff.widen()
		r.notice = msg.err.Error()

		return r.loadWholeCmd(s)
	}

	drewNothing := errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded)

	// A narrowed diff drawing nothing is an answer about the agent rather than a reason to leave,
	// so the whole change comes back with the answer said out loud.
	if drewNothing && (r.diff.base != "" || r.diff.answer != nil) {
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

	if msg.notice != "" {
		r.notice = msg.notice
	}

	r.root = msg.patch.Root
	r.round = msg.round
	r.diff.load(msg, s.isDark, s.glyphs)
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
func (d *reviewDiff) load(msg reviewLoadedMsg, isDark bool, glyphs glyph.Set) {
	d.baseRound = msg.earlier
	d.baseAt = msg.earlierAt
	d.pinned = msg.pinned
	d.chapters = msg.chapters
	d.rounds = msg.rounds
	d.since = msg.since

	if d.answer != nil {
		d.base, d.head = msg.answerBase, msg.answerHead
		d.baseLabel = "the answer to this comment"
		if msg.replayed {
			d.baseLabel += ", replayed across the rebase"
		}
	}

	// The files the reader opened out are the view's own, so a view built again over the patch
	// takes them from the one it replaces, or from what was kept when that one was dropped.
	view := diffview.NewView(msg.patch, isDark)
	view.SetGlyphs(glyphs)
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
		h.report("Could not write on "+r.agentName, msg.err)

		if msg.unsent != nil {
			return r.restoreMessage(s, *msg.unsent)
		}

		return nil
	}

	r.notice = msg.notice

	switch {
	case !msg.readBack:
	case msg.readErr != nil:
		h.report("Could not read the review of "+r.agentName+" back", msg.readErr)
	default:
		r.applyThreads(s, msg.threads)
	}

	return nil
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
	return r.load(s, false)
}

// reloadFresh reads the worktree as it stands, even where unsent comments hold the diff to the
// round they were written on.
func (r *reviewScreen) reloadFresh(s panelShared) tea.Cmd {
	return r.load(s, true)
}

func (r *reviewScreen) load(s panelShared, fresh bool) tea.Cmd {
	if r.diff.view == nil {
		return nil
	}

	if place, ok := r.diff.view.GetPlace(r.diff.cursor); ok {
		r.diff.restore = &place
	}

	r.diff.followedMove = r.findMovedRound(s)

	return loadReviewCmd(s, loadReviewCmdParams{
		AgentID: r.agentID,
		Dir:     r.dir,
		Exclude: r.exclude,
		Base:    r.diff.base,
		Head:    r.diff.head,
		Answer:  r.diff.answer,
		Fresh:   fresh,
	})
}

func (r *reviewScreen) leave(h host, s panelShared) tea.Cmd {
	if r.hold.atEditor {
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
