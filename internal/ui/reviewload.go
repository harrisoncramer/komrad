package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	"google.golang.org/protobuf/proto"
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
	err      error
}

// reviewChapter is one round of the review as a reader reads it: the work between two of their
// reviews, or between their last one and now.
type reviewChapter struct {
	// FromSHA is the snapshot the chapter starts after, empty for the point the branch left main.
	FromSHA string
	// ToSHA is the snapshot the chapter ends at.
	ToSHA string
	// At is when the review closing the chapter was released, in clock terms, empty for the
	// chapter nobody has reviewed yet.
	At string
	// Ordinal is the round the chapter ends at, which is the newest round whose comments existed
	// while it was the work in hand.
	Ordinal int32
	// Until is when the agent moved past this chapter, in RFC 3339, empty for the chapter nothing
	// follows. Anything said after it was said about later code.
	Until string
	// Comparable is false for a chapter whose two ends hang off different base commits, which a
	// rebase between two reviews leaves behind.
	Comparable bool
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

// stepReviewBase walks back a round, from the whole change to the newest round and on into the
// older ones, and stops on the oldest.
func (m model) stepReviewBase() (tea.Model, tea.Cmd) {
	if m.review.chapter < 0 {
		return m.showReviewChapter(len(m.review.chapters) - 1)
	}

	return m.showReviewChapter(m.review.chapter - 1)
}

// stepReviewBaseForward walks the other way, and stops on the whole change.
func (m model) stepReviewBaseForward() (tea.Model, tea.Cmd) {
	if m.review.chapter < 0 {
		return m, nil
	}

	return m.showReviewChapter(m.review.chapter + 1)
}

// showReviewChapter puts one round of the review on screen, or the whole change past the newest
// round, and stops at either end so a held key walks to the end and stays there.
func (m model) showReviewChapter(at int) (tea.Model, tea.Cmd) {
	held := len(m.review.chapters)
	if held == 0 {
		return m, nil
	}

	// The whole change sits after the newest round rather than before the oldest, since it is
	// the view that carries every round at once.
	switch {
	case at < 0:
		at = 0
	case at >= held:
		at = -1
	}

	if at == m.review.chapter {
		return m, nil
	}

	m.review.chapter = at
	if at < 0 {
		m.widenReviewBase()

		next, cmd := m.reloadReview()

		return next, cmd
	}

	chapter := m.review.chapters[at]
	m.review.base = chapter.FromSHA
	m.review.head = chapter.ToSHA
	m.review.baseLabel = reviewChapterLabel(reviewChapterLabelParams{
		Chapter: chapter,
		At:      at,
		Held:    held,
	})

	next, cmd := m.reloadReview()

	return next, cmd
}

// reviewChapterLabelParams is one chapter and where it sits in the walk.
type reviewChapterLabelParams struct {
	Chapter reviewChapter
	At      int
	Held    int
}

// reviewChapterLabel names a chapter for the band, counting from the oldest so a number keeps
// meaning the same round however many rounds follow it.
func reviewChapterLabel(params reviewChapterLabelParams) string {
	said := fmt.Sprintf("round %d of %d", params.At+1, params.Held)

	switch {
	case !params.Chapter.Comparable:
		return said + ", not comparable, the branch was rebased"
	case params.Chapter.At == "":
		return said + ", not reviewed yet"
	default:
		return said + ", reviewed at " + params.Chapter.At
	}
}

// widenReviewBase puts the diff back to the whole change.
func (m *model) widenReviewBase() {
	m.review.base = ""
	m.review.head = ""
	m.review.baseLabel = ""
	m.review.chapter = -1
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

	return m.doesReviewWalk()
}

// reviewBaseDesc says which way the base key will move, in the legend's own width budget.
func (m model) reviewBaseDesc() string {
	return "an earlier round"
}

// reviewBaseForwardDesc is the same for the key going the other way.
func (m model) reviewBaseForwardDesc() string {
	return "a later round"
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

		round, pinned := pendingRound(ctx, agents, pendingRoundParams{AgentID: agentID})
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

// releasedAtOf is the clock time the person last let a review go, which is what the screen says
// instead of a round number.
func releasedAtOf(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
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

	return releasedClock(last.GetReleasedAt())
}

// releasedClock is when a review was let go of, in the terms a reader names it by.
func releasedClock(released string) string {
	stamp, err := time.Parse(time.RFC3339, released)
	if err != nil {
		return ""
	}

	return stamp.Local().Format("3:04PM")
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

// reviewChapters is the review broken at the reviews the person released, oldest first, so a
// reader can step through the work a round at a time rather than only seeing the whole of it.
func reviewChapters(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	round *agentv1.ReviewRound,
) []reviewChapter {
	reviews, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return nil
	}

	rounds, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return nil
	}

	return chapterList(chapterListParams{
		Rounds:   rounds.Msg.GetRounds(),
		Reviews:  reviews.Msg.GetReviews(),
		OnScreen: round,
	})
}

// chapterListParams is everything the chapters are cut from, kept apart from the calls that read
// it so the cutting can be exercised on its own.
type chapterListParams struct {
	Rounds   []*agentv1.ReviewRound
	Reviews  []*agentv1.Review
	OnScreen *agentv1.ReviewRound
}

// chapterList cuts the rounds at every round the person released a review against, and ends the
// last chapter at the round on screen.
func chapterList(params chapterListParams) []reviewChapter {
	closed := map[int32]string{}
	for _, review := range params.Reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if released := review.GetReleasedAt(); released != "" {
			closed[review.GetRoundOrdinal()] = releasedClock(released)
		}
	}

	milestones := make([]*agentv1.ReviewRound, 0, len(closed)+1)
	for _, candidate := range params.Rounds {
		if candidate.GetOrdinal() > params.OnScreen.GetOrdinal() {
			continue
		}
		if _, ok := closed[candidate.GetOrdinal()]; ok {
			milestones = append(milestones, candidate)
		}
	}

	// The work nobody has reviewed yet is a chapter of its own, and it is the one a reader
	// arrives wanting, so it is only left out when the newest review closed the round on screen.
	if _, ok := closed[params.OnScreen.GetOrdinal()]; !ok {
		milestones = append(milestones, params.OnScreen)
	}

	if len(milestones) < 2 {
		return nil
	}

	chapters := make([]reviewChapter, 0, len(milestones))
	from := ""
	fromBase := params.OnScreen.GetBaseSha()
	for _, milestone := range milestones {
		chapters = append(chapters, reviewChapter{
			FromSHA:    from,
			ToSHA:      milestone.GetSnapshotSha(),
			At:         closed[milestone.GetOrdinal()],
			Ordinal:    milestone.GetOrdinal(),
			Until:      roundAfter(params.Rounds, milestone.GetOrdinal()),
			Comparable: from == "" || fromBase == milestone.GetBaseSha(),
		})

		from, fromBase = milestone.GetSnapshotSha(), milestone.GetBaseSha()
	}

	return chapters
}

// threadsByChapterParams is the whole conversation and which round of it is on screen.
type threadsByChapterParams struct {
	Threads  []*agentv1.ReviewThread
	Chapters []reviewChapter
	At       int
}

// threadsByChapter drops the comments written after the round on screen, and gives back the
// whole conversation while the whole change is on screen.
func threadsByChapter(params threadsByChapterParams) []*agentv1.ReviewThread {
	if params.At < 0 || params.At >= len(params.Chapters) {
		return params.Threads
	}

	chapter := params.Chapters[params.At]

	kept := make([]*agentv1.ReviewThread, 0, len(params.Threads))
	for _, thread := range params.Threads {
		if thread.GetRoundOrdinal() > chapter.Ordinal {
			continue
		}

		kept = append(kept, messagesByChapter(thread, chapter.Until))
	}

	return kept
}

// messagesByChapter is one thread with the replies written after the chapter closed left out, so a
// conversation that carried on about later code is read as it stood then.
func messagesByChapter(thread *agentv1.ReviewThread, until string) *agentv1.ReviewThread {
	if until == "" {
		return thread
	}

	said := thread.GetMessages()

	kept := make([]*agentv1.ReviewMessage, 0, len(said))
	for _, message := range said {
		if isBefore(message.GetCreatedAt(), until) {
			kept = append(kept, message)
		}
	}

	if len(kept) == len(said) {
		return thread
	}

	// The threads belong to the model rather than to the view, so the shortened one is a copy.
	trimmed, ok := proto.Clone(thread).(*agentv1.ReviewThread)
	if !ok {
		return thread
	}

	trimmed.Messages = kept

	return trimmed
}

// isBefore reports whether one stamp came before another, parsed rather than compared as text,
// since two stamps written at different offsets sort by their offset instead of their moment.
func isBefore(stamp, mark string) bool {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}

	against, err := time.Parse(time.RFC3339, mark)
	if err != nil {
		return false
	}

	return at.Before(against)
}

// roundAfter is when the round following an ordinal was taken, which is the moment the agent left
// that round behind, and nothing when it is the newest.
func roundAfter(rounds []*agentv1.ReviewRound, ordinal int32) string {
	after := ""
	for _, round := range rounds {
		if round.GetOrdinal() <= ordinal {
			continue
		}
		if after == "" || isBefore(round.GetCreatedAt(), after) {
			after = round.GetCreatedAt()
		}
	}

	return after
}

// awaitingReviewAnswer reports whether the person has released a review against the round on
// screen, which is the state the service refuses a second review in.
func awaitingReviewAnswer(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	round *agentv1.ReviewRound,
) bool {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return false
	}

	for _, review := range resp.Msg.GetReviews() {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() != "" && review.GetRoundOrdinal() == round.GetOrdinal() {
			return true
		}
	}

	return false
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

// pendingRoundParams says whose review is being read.
type pendingRoundParams struct {
	AgentID string
}

// pendingRound is the round a reader's own open batch was opened against, and nothing when they
// have none, so a reader part way through a review keeps reading the snapshot they started on
// rather than being moved onto whatever the agent has written since.
func pendingRound(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	params pendingRoundParams,
) (*agentv1.ReviewRound, bool) {
	// Explainer: the batch says which snapshot the review is being written against, and a thread
	// does not. Replying into a thread from an earlier round leaves an unsent message on that
	// thread, so reading the round off the thread would drag the whole diff back to whenever that
	// conversation started.
	roundID := openBatchRound(ctx, agents, params.AgentID)
	if roundID == "" {
		return nil, false
	}

	rounds, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: params.AgentID,
	}))
	if err != nil {
		return nil, false
	}

	for _, round := range rounds.Msg.GetRounds() {
		if round.GetId() == roundID {
			return round, true
		}
	}

	return nil, false
}

// openBatchRound is the round the person's unreleased review was opened against, empty when they
// have nothing open.
func openBatchRound(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) string {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return ""
	}

	for _, review := range resp.Msg.GetReviews() {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() == "" {
			return review.GetRoundId()
		}
	}

	return ""
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
	m.review.chapters = msg.chapters
	m.review.root = msg.patch.Root
	m.review.view = review.NewView(msg.patch, m.isDark)
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
