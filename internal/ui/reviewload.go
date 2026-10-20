package ui

import (
	"context"
	"errors"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

const reviewEditorHeight = 4

// isReviewable reports whether an agent has work of its own to read, in a directory still there.
func (l listing) isReviewable(agent panel.Agent) bool {
	return agent.Cwd != "" && !l.tasks.findForAgent(agent).WorktreeRemoved && agent.Role != panel.RoleReviewer
}

type reviewLoadedMsg struct {
	agentID string
	// load is the read of the diff this message answers.
	load     int
	snapshot *agentv1.ReviewSnapshot
	patch    *diff.Patch
	threads  []*agentv1.ReviewThread
	viewed   []*agentv1.ReviewViewedFile
	// since says how much of this snapshot is new to the reader.
	since string
	// earlier is the snapshot the person last released a review against, nil when there is none.
	earlier *agentv1.ReviewSnapshot
	// earlierAt is when they released it, in clock terms, empty when there is none.
	earlierAt string
	snapshots []*agentv1.ReviewSnapshot
	reviews   []*agentv1.Review
	// base and head are the commits the diff was read between.
	base string
	head string
	// replayed says the older side was carried across a rebase onto the newer one's base.
	replayed  bool
	conflicts []string
	// answerBase and answerHead are the snapshots an answer on screen was measured between.
	answerBase     string
	answerHead     string
	originBase     string
	originHead     string
	originFellBack bool
	// notice is what the load has to say about the snapshot it settled on.
	notice  string
	widened string
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
		loads:     r.loads,
		taskID:    agent.TaskID,
		agentName: agent.Name,
		dir:       agent.Cwd,
		exclude:   worktree.DiffExclude,
		loading:   true,
		diff:      reviewDiff{spec: wholeChange()},
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

	*r = reviewScreen{loads: r.loads}

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
func (r *reviewScreen) loadWholeCmd(m *model) tea.Cmd {
	r.loads++

	return loadReviewCmd(m, loadReviewCmdParams{
		AgentID: r.agentID,
		TaskID:  r.taskID,
		Dir:     r.dir,
		Exclude: r.exclude,
		Load:    r.loads,
	})
}

// loadReviewCmdParams says which task's snapshot to open, for which agent on screen, where to read
// it, and what to measure it from.
type loadReviewCmdParams struct {
	AgentID string
	TaskID  string
	Dir     string
	Exclude []string
	Spec    diffSpec
	Load    int
}

// loadReviewCmd opens a snapshot of the agent and reads that rather than the worktree, a tree that
// has not moved giving back the snapshot already taken of it.
func loadReviewCmd(m *model, params loadReviewCmdParams) tea.Cmd {
	agents := m.api.reads().agents
	agentID := params.AgentID
	patches := m.patches

	read := func(ctx context.Context) reviewLoadedMsg {
		resp, err := agents.OpenReviewHistory(ctx, connect.NewRequest(historyRequestOf(params)))
		if err != nil {
			return reviewLoadedMsg{err: err}
		}

		opened := resp.Msg
		resolved := opened.GetResolved()

		loaded, err := loadSnapshotPatch(ctx, loadSnapshotPatchParams{
			Patches:  patches,
			Dir:      params.Dir,
			Exclude:  params.Exclude,
			Resolved: resolved,
		})
		if err != nil {
			return reviewLoadedMsg{
				snapshot: opened.GetSnapshot(),
				widened:  opened.GetWidened(),
				err:      err,
			}
		}

		viewed, err := listReviewViewedFiles(ctx, agents, params.TaskID)

		return reviewLoadedMsg{
			snapshot:       opened.GetSnapshot(),
			patch:          loaded.patch,
			threads:        opened.GetThreads(),
			viewed:         viewed,
			earlier:        findSnapshot(opened.GetSnapshots(), opened.GetEarlierSnapshotId()),
			earlierAt:      reviewTimeOf(opened.GetEarlierReleasedAt()),
			snapshots:      opened.GetSnapshots(),
			reviews:        opened.GetReviews(),
			base:           resolved.GetBaseSha(),
			head:           resolved.GetHeadSha(),
			replayed:       resolved.GetReplayed(),
			conflicts:      resolved.GetConflicts(),
			traced:         tracedOf(opened.GetTraced()),
			answerBase:     resolved.GetAnswerBaseSha(),
			answerHead:     resolved.GetAnswerHeadSha(),
			originBase:     loaded.origin.base,
			originHead:     loaded.origin.head,
			originFellBack: loaded.origin.fellBack,
			notice:         opened.GetNotice(),
			widened:        opened.GetWidened(),
			since:          changedSinceSaid(opened.GetChangedSinceEarlier()),
			err:            err,
		}
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		msg := read(ctx)
		msg.agentID, msg.load = agentID, params.Load

		return msg
	}
}

func historyRequestOf(params loadReviewCmdParams) *agentv1.OpenReviewHistoryRequest {
	req := &agentv1.OpenReviewHistoryRequest{
		Task: params.TaskID,
	}

	spec := params.Spec
	switch {
	case spec.answer != nil:
		req.Span = &agentv1.OpenReviewHistoryRequest_Answer{Answer: &agentv1.ReviewAnswerSpan{
			FromSnapshotId: spec.answer.FromSnapshot,
			ToSnapshotId:   spec.answer.ToSnapshot,
		}}
	case spec.origin != nil:
		req.Span = &agentv1.OpenReviewHistoryRequest_Origin{Origin: &agentv1.ReviewOriginSpan{
			SnapshotId: spec.origin.SnapshotID,
			BaseSha:    spec.origin.BaseSHA,
		}}
	case !spec.isWholeChange():
		req.Span = &agentv1.OpenReviewHistoryRequest_Snapshots{Snapshots: &agentv1.ReviewSnapshotSpan{
			FromSnapshotId: nilIfBlank(spec.from),
			ToSnapshotId:   nilIfBlank(spec.to),
		}}
	}

	return req
}

func tracedOf(places map[string]*agentv1.ReviewTracedPlace) map[string]diffview.Traced {
	traced := make(map[string]diffview.Traced, len(places))
	for id, place := range places {
		traced[id] = diffview.Traced{
			FilePath: place.GetFilePath(),
			NewStart: place.GetNewStart(),
			FileOnly: place.GetFileOnly(),
			Exact:    place.GetExact(),
		}
	}

	return traced
}

// changedSinceSaid says how much of this snapshot is new since the person last released a review,
// saying nothing at all when none of it is.
func changedSinceSaid(changed int32) string {
	if changed <= 0 {
		return ""
	}

	return label.Counted(int(changed), "file") + " changed since you last reviewed"
}

type snapshotPatch struct {
	patch  *diff.Patch
	origin originDiff
}

type loadSnapshotPatchParams struct {
	Patches  diff.Reader
	Dir      string
	Exclude  []string
	Resolved *agentv1.ReviewResolvedSpan
}

func loadSnapshotPatch(ctx context.Context, params loadSnapshotPatchParams) (*snapshotPatch, error) {
	resolved := params.Resolved
	loading := diff.LoadParams{
		Dir:         params.Dir,
		BaseSHA:     resolved.GetBaseSha(),
		SnapshotSHA: resolved.GetHeadSha(),
		Exclude:     params.Exclude,
	}

	origin := originDiff{
		base:         resolved.GetOriginBaseSha(),
		head:         resolved.GetOriginHeadSha(),
		snapshotBase: resolved.GetOriginSnapshotBaseSha(),
	}
	patch, err := diff.Load(ctx, params.Patches, loading)
	if err != nil && origin.canFallBack() && isUnreadableBase(ctx, err) {
		origin.base, origin.fellBack = origin.snapshotBase, true
		loading.BaseSHA = origin.base
		patch, err = diff.Load(ctx, params.Patches, loading)
	}
	if err != nil {
		return nil, err
	}

	return &snapshotPatch{patch: patch, origin: origin}, nil
}

func (r *reviewScreen) applyLoaded(m *model, msg reviewLoadedMsg) tea.Cmd {
	if msg.agentID != r.agentID || msg.load != r.loads {
		return nil
	}

	r.diff.snapshots, r.diff.reviews, r.diff.newest = msg.snapshots, msg.reviews, msg.snapshot

	if msg.widened != "" {
		r.diff.widen()
		m.note(messageWarning, msg.widened)
	}

	isEmpty := errors.Is(msg.err, diff.ErrNoChanges) || errors.Is(msg.err, diff.ErrAllExcluded)

	// A narrowed diff with nothing in it is an answer about the agent rather than a reason to leave,
	// so the whole change comes back with the answer said out loud.
	if isEmpty && !r.diff.spec.isWholeChange() {
		said := "nothing written over " + r.diff.rangeSaid()
		if r.diff.spec.origin != nil {
			said = "nothing to render where this comment was written"
		}

		r.diff.widen()
		m.note(messageWarning, said)

		return r.loadWholeCmd(m)
	}

	// Both render nothing, so both back out rather than leaving the reader in an empty viewer.
	if isEmpty {
		return r.refuse(m, msg.err)
	}

	if msg.patch == nil && r.diff.spec.origin != nil {
		r.diff.widen()
		m.note(messageWarning, "could not render where this comment was written: "+msg.err.Error())

		return r.loadWholeCmd(m)
	}

	if msg.patch == nil {
		return r.abandon(m, msg.err)
	}

	r.loading = false
	if msg.err != nil {
		m.report("Could not read the whole review of "+r.agentName, msg.err)
	}

	r.notice = msg.notice

	r.root = msg.patch.Root
	r.snapshot = msg.snapshot
	r.diff.load(msg, m.term.isDark, m.glyphs)
	r.threads.set(msg.threads)

	r.scrollToCursor(m)
	r.followNotices(m)

	return nil
}

// load renders a patch just read, over the one on screen, and puts the cursor back where the
// reader was standing.
func (d *reviewDiff) load(msg reviewLoadedMsg, isDark bool, glyphs glyph.Set) {
	d.baseSnapshot = msg.earlier
	d.baseAt = msg.earlierAt
	d.since = msg.since

	d.spec.settle(msg)

	// The files the reader opened out are the view's own, so a view built again over the patch
	// takes them from the one it replaces, or from what was kept when that one was dropped.
	view := diffview.NewView(msg.patch, isDark)
	view.SetGlyphs(glyphs)
	view.SetNarrowed(!d.spec.isWholeChange())
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

// applyThreads renders the review as it now stands, keeping the reader where they were.
func (r *reviewScreen) applyThreads(m *model, threads []*agentv1.ReviewThread) {
	r.threads.set(threads)

	rendered := r.diff.threadsOnScreen(threads)

	// The cursor's place is read before the threads are pinned again, which rebuilds the rows
	// a settled or deleted thread takes away from under it.
	place, ok := r.diff.View().GetPlace(r.diff.Cursor())
	r.diff.View().PinThreads(rendered)
	r.diff.Keep(place, ok)

	r.scrollToCursor(m)
}

// toggleAgentThreads shows or hides the conversations agents opened.
func (r *reviewScreen) toggleAgentThreads(m *model) tea.Cmd {
	r.diff.hidesAgentThreads = !r.diff.hidesAgentThreads

	if r.diff.View() != nil {
		r.applyThreads(m, r.threads.list)
	}

	return nil
}

// catchUp takes a snapshot of the worktree as it stands and ends the range on it.
func (r *reviewScreen) catchUp(m *model) tea.Cmd {
	if r.diff.spec.isRange() {
		r.diff.spec.to = ""
	}

	return r.reload(m)
}

// reload reads the diff again, holding the place the cursor is standing so the diff comes back
// under it.
func (r *reviewScreen) reload(m *model) tea.Cmd {
	if r.diff.View() == nil {
		return nil
	}

	if place, ok := r.diff.View().GetPlace(r.diff.Cursor()); ok {
		r.diff.restore = &place
	}

	r.diff.followedMove = r.findMovedSnapshot(m)
	r.loads++

	return loadReviewCmd(m, loadReviewCmdParams{
		AgentID: r.agentID,
		TaskID:  r.taskID,
		Dir:     r.dir,
		Exclude: r.exclude,
		Spec:    r.diff.spec,
		Load:    r.loads,
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

// backFromDiff hands the keys to the file tree, or leaves the screen where no tree renders.
func (r *reviewScreen) backFromDiff(m *model) tea.Cmd {
	if r.hold.isHeld() || !r.isBlockRendered(m) {
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
