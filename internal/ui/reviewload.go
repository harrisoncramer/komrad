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
	answerBase     string
	answerHead     string
	originBase     string
	originHead     string
	originFellBack bool
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
	write  int
}

// open puts the review on an agent's diff and reads it.
func (r *reviewScreen) open(m *model, agent panel.Agent) tea.Cmd {
	m.dropMessage(r.notices.moved)

	worktree := m.listing.worktreeFor(agent)

	*r = reviewScreen{
		agentID:   agent.ID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		exclude:   worktree.DiffExclude,
		loading:   true,
		// The whole change is what a review opens on, and it sits before the first round rather
		// than on it, so the first press of the step key lands on the newest round.
		diff: reviewDiff{spec: wholeChange()},
	}

	return r.loadWholeCmd(m)
}

// refuse backs out to the listing, for a worktree that turned out to hold nothing.
func (r *reviewScreen) refuse(m *model, err error) tea.Cmd {
	name := r.agentName
	letGo := r.drop(m)
	m.reportBrief(describeRefusedReview(name, err), err.Error())

	return letGo
}

// abandon backs out to the listing for a diff that could not be read at all.
func (r *reviewScreen) abandon(m *model, err error) tea.Cmd {
	name := r.agentName
	letGo := r.drop(m)
	m.report("Could not read the diff of "+name, err)

	return letGo
}

// drop puts the reader back on the row they opened the review from, letting go of an agent
// stopped for the editor.
func (r *reviewScreen) drop(m *model) tea.Cmd {
	letGo := r.endHold(m, abandonedSaid)

	*r = reviewScreen{}

	return tea.Batch(m.enter(screenAgents, screenTarget{}), letGo)
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
func (r reviewScreen) loadWholeCmd(m *model) tea.Cmd {
	return loadReviewCmd(m, loadReviewCmdParams{
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
	Spec    diffSpec
	Fresh   bool
}

// loadReviewCmd opens a round of the agent and reads that rather than the worktree, a tree that
// has not moved giving back the round already taken of it.
func loadReviewCmd(m *model, params loadReviewCmdParams) tea.Cmd {
	agents := m.api.reads().agents
	agentID, dir, exclude := params.AgentID, params.Dir, params.Exclude
	repo := m.git
	_, live := m.listing.liveAgent(agentID)
	held := assemble.ReviewThreads(m.listing.world, agentID)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		record, err := readReviewRecord(ctx, readReviewRecordParams{
			Agents:  agents,
			AgentID: agentID,
			Live:    live,
			Held:    held,
		})
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		span, err := resolveReviewRange(record.rounds, params.Spec)
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		picked, err := pickReviewRound(ctx, pickReviewRoundParams{
			Agents:  agents,
			AgentID: agentID,
			Record:  record,
			Fresh:   params.Fresh,
		})
		if err != nil {
			return reviewLoadedMsg{agentID: agentID, err: err}
		}

		round, rounds := picked.round, picked.rounds

		loaded, err := loadRoundPatch(ctx, loadRoundPatchParams{
			Git:     repo,
			Dir:     dir,
			Exclude: exclude,
			Round:   round,
			Range:   span,
		})
		if err != nil {
			return reviewLoadedMsg{
				agentID: agentID,
				round:   round,
				err:     err,
			}
		}

		viewed, err := listReviewViewedFiles(ctx, agents, agentID)
		earlier := findPreviousReviewedRound(record.reviews, rounds, round)
		chapters := replayChapters(ctx, replayChaptersParams{
			Git: repo,
			Dir: dir,
			Chapters: cutChapters(cutChaptersParams{
				Rounds:   rounds,
				Reviews:  record.reviews,
				OnScreen: round,
			}),
		})
		traced := traceThreads(ctx, traceThreadsParams{
			Git:     repo,
			Dir:     dir,
			ToSHA:   loaded.head,
			Threads: record.threads,
			Rounds:  rounds,
		})

		return reviewLoadedMsg{
			agentID:        agentID,
			round:          round,
			patch:          loaded.patch,
			threads:        record.threads,
			viewed:         viewed,
			earlier:        earlier,
			earlierAt:      releasedAtOf(record.reviews, round),
			pinned:         picked.pinned,
			chapters:       chapters,
			rounds:         rounds,
			replayed:       loaded.replayed,
			traced:         traced,
			answerBase:     span.answerBase,
			answerHead:     span.answerHead,
			originBase:     loaded.origin.base,
			originHead:     loaded.origin.head,
			originFellBack: loaded.origin.fellBack,
			notice:         picked.notice,
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

type reviewRecord struct {
	threads   []*agentv1.ReviewThread
	reviews   []*agentv1.Review
	rounds    []*agentv1.ReviewRound
	openBatch string
}

// readReviewRecordParams says whose review record to read, and which threads the world already holds.
type readReviewRecordParams struct {
	Agents  agentReads
	AgentID string
	// Live says the agent's threads are the world's.
	Live bool
	Held []*agentv1.ReviewThread
}

func readReviewRecord(ctx context.Context, params readReviewRecordParams) (*reviewRecord, error) {
	threads := params.Held
	if !params.Live {
		listed, err := listReviewThreads(ctx, params.Agents, params.AgentID)
		if err != nil {
			return nil, err
		}

		threads = listed
	}

	reviews, err := listReviews(ctx, params.Agents, params.AgentID)
	if err != nil {
		return nil, err
	}

	// The rounds are a better answer where they can be had rather than one the screen depends
	// on, so only the pin, which cannot be placed without them, gives up when they cannot.
	rounds, roundsErr := listReviewRounds(ctx, params.Agents, params.AgentID)

	openBatch := openBatchRoundID(reviews)
	if openBatch != "" && roundsErr != nil {
		return nil, roundsErr
	}

	return &reviewRecord{
		threads:   threads,
		reviews:   reviews,
		rounds:    rounds,
		openBatch: openBatch,
	}, nil
}

type reviewRange struct {
	base       string
	head       string
	fromBase   string
	toBase     string
	answerBase string
	answerHead string
	origin     originDiff
}

func resolveReviewRange(rounds []*agentv1.ReviewRound, spec diffSpec) (*reviewRange, error) {
	span := &reviewRange{base: spec.base, head: spec.head}
	if answer := spec.answer; answer != nil {
		from, to := findRound(rounds, answer.FromRound), findRound(rounds, answer.ToRound)
		if from == nil || to == nil {
			return nil, errAnswerGone
		}

		from = findAnswerBase(rounds, from, to)

		span.answerBase, span.answerHead = from.GetSnapshotSha(), to.GetSnapshotSha()
		span.base, span.head = span.answerBase, span.answerHead
		span.fromBase, span.toBase = from.GetBaseSha(), to.GetBaseSha()
	}

	if spec.origin != nil {
		found, ok := findOriginDiff(rounds, spec.origin)
		if !ok {
			return nil, errCommentRoundGone
		}

		span.origin = found
		span.base, span.head = found.base, found.head
	}

	return span, nil
}

type pickedRound struct {
	round  *agentv1.ReviewRound
	rounds []*agentv1.ReviewRound
	pinned bool
	notice string
}

type pickReviewRoundParams struct {
	Agents  agentReads
	AgentID string
	Record  *reviewRecord
	Fresh   bool
}

func pickReviewRound(ctx context.Context, params pickReviewRoundParams) (*pickedRound, error) {
	picked := &pickedRound{rounds: params.Record.rounds}
	if !params.Fresh {
		picked.round, picked.pinned = findPendingRound(picked.rounds, params.Record.openBatch)
	}

	if picked.round != nil {
		return picked, nil
	}

	opened, err := params.Agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: params.AgentID,
	}))
	switch {
	case connect.CodeOf(err) == connect.CodeFailedPrecondition && len(picked.rounds) > 0:
		picked.round = picked.rounds[len(picked.rounds)-1]
		picked.notice = "a rebase or merge is part way through, so this is the work as it stood before it"
	case err != nil:
		return nil, err
	default:
		picked.round = opened.Msg.GetRound()
		picked.rounds = withRound(picked.rounds, picked.round)
	}

	return picked, nil
}

type roundPatch struct {
	patch    *diff.Patch
	head     string
	replayed bool
	origin   originDiff
}

type loadRoundPatchParams struct {
	Git     ReviewGit
	Dir     string
	Exclude []string
	Round   *agentv1.ReviewRound
	Range   *reviewRange
}

func loadRoundPatch(ctx context.Context, params loadRoundPatchParams) (*roundPatch, error) {
	base := params.Round.GetBaseSha()
	if params.Range.base != "" {
		base = params.Range.base
	}

	head := params.Round.GetSnapshotSha()
	if params.Range.head != "" {
		head = params.Range.head
	}

	compared := compareRounds(ctx, compareRoundsParams{
		Git:      params.Git,
		Dir:      params.Dir,
		FromSHA:  base,
		FromBase: params.Range.fromBase,
		ToBase:   params.Range.toBase,
	})
	if !compared.Comparable {
		return nil, &roundsApartError{Conflicts: compared.Conflicts}
	}

	loading := diff.LoadParams{
		Dir:         params.Dir,
		BaseSHA:     compared.FromSHA,
		SnapshotSHA: head,
		Exclude:     params.Exclude,
	}

	origin := params.Range.origin
	patch, err := diff.Load(ctx, params.Git, loading)
	if err != nil && origin.canFallBack() && isUnreadableBase(ctx, err) {
		origin.base, origin.fellBack = origin.roundBase, true
		loading.BaseSHA = origin.base
		patch, err = diff.Load(ctx, params.Git, loading)
	}
	if err != nil {
		return nil, err
	}

	return &roundPatch{
		patch:    patch,
		head:     head,
		replayed: compared.Replayed,
		origin:   origin,
	}, nil
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

func (r *reviewScreen) applyLoaded(m *model, msg reviewLoadedMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	var apart *roundsApartError
	if errors.As(msg.err, &apart) || errors.Is(msg.err, errAnswerGone) || errors.Is(msg.err, errCommentRoundGone) {
		r.diff.widen()
		m.note(messageWarning, msg.err.Error())

		return r.loadWholeCmd(m)
	}

	drewNothing := errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded)

	// A narrowed diff drawing nothing is an answer about the agent rather than a reason to leave,
	// so the whole change comes back with the answer said out loud.
	if drewNothing && !r.diff.spec.isWholeChange() {
		said := "nothing written " + r.diff.spec.label
		if r.diff.spec.origin != nil {
			said = "nothing to draw where this comment was written"
		}

		r.diff.widen()
		m.note(messageWarning, said)

		return r.loadWholeCmd(m)
	}

	// Both draw nothing, so both back out rather than leaving the reader in an empty viewer.
	if drewNothing {
		return r.refuse(m, msg.err)
	}

	if msg.patch == nil && r.diff.spec.origin != nil {
		r.diff.widen()
		m.note(messageWarning, "could not draw where this comment was written: "+msg.err.Error())

		return r.loadWholeCmd(m)
	}

	if msg.patch == nil {
		return r.abandon(m, msg.err)
	}

	r.loading = false
	if msg.err != nil {
		m.report("Could not read the whole review of "+r.agentName, msg.err)
	}

	if r.diff.spec.chapter >= len(msg.chapters) {
		r.diff.chapters = msg.chapters
		r.diff.widen()
		r.threads.cancelMessage()
		r.diff.drop()
		r.loading = true

		return r.loadWholeCmd(m)
	}

	r.notice = msg.notice

	r.root = msg.patch.Root
	r.round = msg.round
	r.diff.load(msg, m.term.isDark, m.glyphs)
	r.threads.set(msg.threads)

	r.scrollToCursor(m)
	r.followNotices(m)

	return nil
}

// drop puts the patch down until the next one arrives, keeping the files drawn whole for it.
func (d *reviewDiff) drop() {
	if d.View() != nil {
		d.expanded = d.View().GetExpanded()
	}

	d.Drop()
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

	d.spec.settle(msg)

	// The files the reader opened out are the view's own, so a view built again over the patch
	// takes them from the one it replaces, or from what was kept when that one was dropped.
	view := diffview.NewView(msg.patch, isDark)
	view.SetGlyphs(glyphs)
	view.SetNarrowed(d.spec.base != "")
	view.SetTraced(msg.traced)
	view.SetViewed(indexViewedFiles(msg.viewed, msg.patch))
	if d.View() != nil {
		d.expanded = d.View().GetExpanded()
	}

	view.SetExpanded(d.expanded)
	d.expanded = nil

	view.PinThreads(d.threadsOnScreen(msg.threads))

	if place, ok := d.originPlace(view); ok {
		d.restore = &place
	}

	if place, ok := d.landPlace(view); ok {
		d.restore = &place
	}

	// The pattern is the screen's rather than the view's, so a view built from nothing is given
	// it back once the marks and the threads have settled the rows it walks.
	view.SetPattern(d.filter.pattern)
	d.Replace(view, d.restore)
	d.restore = nil
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

func (r *reviewScreen) applyWrote(m *model, msg reviewWroteMsg) tea.Cmd {
	if msg.agentID != r.agentID || r.diff.View() == nil {
		return nil
	}

	if msg.err != nil {
		m.report("Could not write on "+r.agentName, msg.err)

		if msg.unsent != nil {
			return r.restoreMessage(m, *msg.unsent)
		}

		return nil
	}

	if msg.notice != "" {
		m.note(messageSuccess, msg.notice)
	}

	switch {
	case !msg.readBack:
	case msg.readErr != nil:
		m.report("Could not read the review of "+r.agentName+" back", msg.readErr)
	default:
		r.applyThreads(m, msg.threads)
	}

	return nil
}

// applyThreads draws the review as it now stands, keeping the reader where they were.
func (r *reviewScreen) applyThreads(m *model, threads []*agentv1.ReviewThread) {
	r.threads.set(threads)

	drawn := r.diff.threadsOnScreen(threads)

	// Where the cursor is standing is taken before the threads are pinned again, which rebuilds
	// the rows a settled or deleted thread takes away from under it.
	place, ok := r.diff.View().GetPlace(r.diff.Cursor())
	r.diff.View().PinThreads(drawn)
	r.diff.Keep(place, ok)

	r.scrollToCursor(m)
}

// toggleAgentThreads shows or hides the conversations agents opened.
func (r *reviewScreen) toggleAgentThreads(m *model) tea.Cmd {
	r.diff.showsAgentThreads = !r.diff.showsAgentThreads

	if r.diff.View() != nil {
		r.applyThreads(m, r.threads.list)
	}

	return nil
}

// reload reads the worktree again, holding the place the cursor is standing so the diff comes
// back under it.
func (r *reviewScreen) reload(m *model) tea.Cmd {
	return r.load(m, false)
}

// reloadFresh reads the worktree as it stands, even where unsent comments hold the diff to the
// round they were written on.
func (r *reviewScreen) reloadFresh(m *model) tea.Cmd {
	return r.load(m, true)
}

func (r *reviewScreen) load(m *model, fresh bool) tea.Cmd {
	if r.diff.View() == nil {
		return nil
	}

	if place, ok := r.diff.View().GetPlace(r.diff.Cursor()); ok {
		r.diff.restore = &place
	}

	r.diff.followedMove = r.findMovedRound(m)

	return loadReviewCmd(m, loadReviewCmdParams{
		AgentID: r.agentID,
		Dir:     r.dir,
		Exclude: r.exclude,
		Spec:    r.diff.spec,
		Fresh:   fresh,
	})
}

func (r *reviewScreen) back(m *model) tea.Cmd {
	if r.hold.atEditor {
		return r.closeEdit(m)
	}

	// The drop is the one way off this screen, and it puts the reader back on the row they came
	// from.
	return r.drop(m)
}

// backFromDiff hands the keys to the file tree, or leaves the screen where no tree is drawn.
func (r *reviewScreen) backFromDiff(m *model) tea.Cmd {
	if r.hold.isHeld() || !r.isBlockDrawn(m) {
		return r.back(m)
	}

	r.focusBlock(m)

	return nil
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
