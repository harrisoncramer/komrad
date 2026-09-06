package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/config"
	"weave/internal/diff"
	"weave/internal/review"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

var reviewStagePath = filepath.Join(os.TempDir(), "weave-review.ansi")

const reviewTimeout = 30 * time.Second

// reviewPanStep is the smallest sideways jump, used until the pane has been measured.
const reviewPanStep = 8

const reviewEditorHeight = 4

// reviewState is the review of one agent's diff, while that screen is up.
type reviewState struct {
	agentID   string
	agentName string
	dir       string
	root      string
	exclude   []string
	// round is the snapshot being reviewed, which is frozen: nothing the cursor stands on can
	// move until a new round is opened.
	round   *agentv1.ReviewRound
	view    *review.View
	threads []*agentv1.ReviewThread

	// cursor lives here rather than in the stage, since the sidebar draws the thread it stands on.
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor, since
	// deriving it centres the cursor and repaints every row on every move.
	top       int
	frames    *reviewFrames
	composing bool
	editor    textarea.Model

	// filter is the pattern being searched for in the diff, kept apart from the agents list's own
	// filter so a pattern meant for code does not empty that list.
	filter filterState

	// pending is the recentring prefix waiting on the key that completes it, which is the one
	// gesture in the panel a rebinding cannot reach, since a binding is one key and this is two.
	pending string

	// pinTop holds the window where zt or zb put it until the cursor moves, since the next repaint
	// would otherwise drag it back to keep the cursor clear of the edge.
	pinTop bool

	// pinFloor is the row a file jump put at the top of the pane, which the window stays at or
	// below while the cursor is inside that file, since the margin would otherwise scroll the file
	// it was jumped away from back into view on the first keypress.
	pinFloor int
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	loading bool
	notice  string
	// since says how much of the diff is new to the reader, which is a standing fact about what
	// is on screen rather than an answer to a keypress, so it outlives the notice slot.
	since string
	err   error

	// restore is where the cursor was standing when the patch was read again, so a reload lands
	// the reader back where they left rather than at the top of the diff.
	restore *review.Place

	// activity is the conversation as it stood when the sidebar was last drawn, which a tick
	// compares against so a refresh that would find nothing new never rebuilds anything.
	activity reviewActivity

	// polling is set while a conversation read is in flight, so a slow one is not asked for
	// again by every tick that passes underneath it.
	polling bool

	// moved is set once the agent has written something since the round on screen was taken, which
	// is a prompt to open the next one rather than anything the panel does on its own.
	moved bool

	// diffPolling is set while a background probe of the worktree is in flight, and diffPolled
	// is when the last one started, since the worktree is asked every few seconds rather than
	// every tick.
	diffPolling bool
	diffPolled  time.Time
}

// reviewActivity is the cheap probe a conversation refresh is decided on, since repinning walks
// every row of the patch and a tick that finds this unmoved does no work at all.
type reviewActivity struct {
	threads  int
	messages int
	// newest is the newest thing said or changed, and the counts beside it are what a stamp
	// cannot see, since deleting a thread moves nothing forward.
	newest string
	// delivery is how many comments stand in each state, which is the half of the answer no
	// stamp reaches: claiming a doorbell writes to mail alone, and a doorbell running out of
	// time writes nowhere at all, so both are read off what the server answered rather than
	// off when a row was last written.
	delivery [reviewDeliveryStates]int
}

// reviewDeliveryStates is how many slots the delivery counts get, one per state the proto
// defines with room over, so a state added there is counted without being named here.
const reviewDeliveryStates = 8

// deliverySlot is which count a state belongs in, folding a state this build has never heard of
// into the unspecified slot, since an enum comes off the wire as whatever number was sent.
func deliverySlot(state agentv1.ReviewDeliveryState) int {
	if state < 0 || int(state) >= reviewDeliveryStates {
		return 0
	}

	return int(state)
}

func reviewActivityOf(threads []*agentv1.ReviewThread) reviewActivity {
	activity := reviewActivity{threads: len(threads)}
	for _, thread := range threads {
		activity.messages += len(thread.GetMessages())
		activity.newest = max(activity.newest, thread.GetUpdatedAt())

		for _, message := range thread.GetMessages() {
			activity.newest = max(activity.newest, message.GetUpdatedAt())
			activity.delivery[deliverySlot(message.GetDelivery())]++
		}
	}

	return activity
}

// reviewFrames is the size of the pane the diff is drawn into, kept here because the frame
// reports it once when it starts the drawer rather than on every repaint.
type reviewFrames struct {
	width  int
	height int
}

// window is the screenful starting at top, with the cursor's own row drawn as the cursor, dim
// while the keys are on the record under the sidebar rather than on the diff.
func (f *reviewFrames) window(view *review.View, top, cursor, pan int, focused bool) string {
	height := max(f.height, 1)

	start := max(min(top, view.Len()-height), 0)

	held := review.LiveCursor
	if !focused {
		held = review.DimCursor
	}

	// Only the rows the pane can show are drawn, since a nine thousand row diff takes a tenth of
	// a second to draw whole and panning would pay that on every keypress.
	rows := make([]string, 0, height)
	for y := start; y < min(start+height, view.Len()); y++ {
		mark := review.NoCursor
		if y == cursor {
			mark = held
		}

		rows = append(rows, view.RenderRow(y, mark, f.width, pan))
	}

	// Padded out rather than left short, so a diff smaller than the pane is followed by blank
	// lines instead of the drawer being handed fewer rows than it has.
	for len(rows) < height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n") + "\n"
}

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
		frames:    &reviewFrames{},
		editor:    editor,
		loading:   true,
	}
	m.frame.Show(layout.RegionStage, newTextContent("_loading the diff…_"))

	return m, m.loadReviewCmd(m.review.agentID, m.review.dir, m.review.exclude)
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
			return reviewLoadedMsg{agentID: agentID, round: round, err: err}
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

	changed, err := diff.ChangedSince(ctx, dir, from, round.GetSnapshotSha(), exclude)
	if err != nil || changed == 0 {
		return ""
	}

	return fmt.Sprintf("%s changed since you last reviewed", files(changed))
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

func files(count int) string {
	if count == 1 {
		return "1 file"
	}

	return fmt.Sprintf("%d files", count)
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

func (m *model) applyReviewLoaded(msg reviewLoadedMsg) {
	if msg.agentID != m.review.agentID {
		return
	}

	m.review.loading = false
	m.review.err = msg.err
	if msg.patch == nil {
		return
	}

	m.review.root = msg.patch.Root
	m.review.view = review.NewView(msg.patch, m.isDark)
	m.review.view.SetViewed(stillViewed(msg.viewed))
	m.review.view.PinThreads(msg.threads)

	// The pattern lives on the ui model rather than the view, so a view built from nothing has
	// to be given it back, once the marks and the threads have settled the rows it walks.
	m.review.view.SetPattern(m.review.filter.pattern)

	m.review.threads = msg.threads
	m.review.activity = reviewActivityOf(msg.threads)
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

	m.stageReview()
}

// stillViewed is which files of the round have been read, and a mark belongs to one round, so a
// later round starts with none of them and nothing has to be compared to know that.
func stillViewed(viewed []*agentv1.ReviewViewedFile) map[string]bool {
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
	m.review.threads = threads
	m.review.activity = reviewActivityOf(threads)

	// Where the cursor is standing is taken before the threads are pinned again, since pinning
	// rebuilds the rows the threads with nowhere to stand are drawn on, and a thread settled or
	// deleted takes one of those rows away from under the cursor.
	place, ok := m.review.view.GetPlace(m.review.cursor)
	m.review.view.PinThreads(threads)
	m.keepReviewCursor(place, ok)

	m.stageReview()
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

func (m *model) stageReview() {
	if m.review.view == nil {
		return
	}

	// Showing is what measures the pane, so the window is only settled afterwards.
	m.frame.Show(layout.RegionStage, m.reviewContent())
	m.scrollReviewToCursor()

	if err := m.repaintReview(); err != nil {
		m.report("Could not draw the diff", err)
	}
}

// repaintReviewFocus redraws the diff after the record has taken the keys or given them back,
// since the cursor row is painted dim while they are away.
func (m *model) repaintReviewFocus() {
	if err := m.repaintReview(); err != nil {
		m.report("Could not draw the diff", err)
	}
}

// repaintReview rewrites the screenful the stage is showing and tells the pager to re-read it.
func (m model) repaintReview() error {
	pane := m.frame.Pane(layout.RegionStage)
	frames := m.review.frames

	// Nothing to draw against until the frame has started the drawer and reported its pane size.
	if pane == "" || m.review.view == nil || frames.width == 0 {
		return nil
	}

	if !writeStageFile(reviewStagePath, frames.window(m.review.view, m.review.top, m.review.cursor, m.review.pan, !m.isDetailsFocused())) {
		return fmt.Errorf("failed to write the diff to %s", reviewStagePath)
	}

	// The pager is left running and re-reads on any key, since restarting it repainted the whole
	// stage as a dozen partial writes over twenty milliseconds and read as a flicker.
	return m.tmux.SendKeys(pane, "R")
}

// reviewContent starts the pager on the diff, keyed by the agent alone so that moving the
// cursor repaints rather than respawning the pane.
func (m model) reviewContent() layout.Content {
	state := m.review

	return layout.Content{
		Key:   "review:" + state.agentID,
		Dir:   homeDir(),
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			state.frames.width = max(target.Width, review.GutterWidth+20)
			state.frames.height = max(target.Height, 1)

			if !writeStageFile(reviewStagePath, state.frames.window(state.view, state.top, state.cursor, state.pan, !m.isDetailsFocused())) {
				return nil, false
			}

			return m.reviewStageArgv(reviewStagePath), true
		},
	}
}

// reviewStageArgv runs weave's own drawer in the stage rather than a pager, since bubbletea's
// renderer writes only the rows that differ where a pager repaints its whole screen.
func (m model) reviewStageArgv(path string) []string {
	self := m.self
	if self == "" {
		self = "weave"
	}

	return []string{self, "stage", path}
}

// doesTakeEveryKey reports whether the review is mid-input, which means a key bound elsewhere in
// the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.composing || r.filter.typing || r.pending != ""
}

func (m model) updateReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.review.composing {
		return m.updateReviewComposer(key)
	}

	if m.review.filter.typing {
		return m.updateReviewFilter(key)
	}

	k := key.String()

	if m.review.pending != "" {
		return m.updateReviewPending(k)
	}
	keys := m.keys.Review

	if m.review.view == nil {
		if isBound(k, keys.Back) {
			return m.leaveReview()
		}

		return m, nil
	}

	m.review.notice = ""

	// The diff opens for an agent that is not there, so the keys that would write to it are
	// refused here rather than left to fail at the server.
	if _, dead := m.reviewIsReadOnly(); dead && isReviewWrite(k, m.keys) {
		return m.refuseReviewWrite()
	}

	switch {
	case isBound(k, keys.Back):
		return m.leaveReview()
	case isBound(k, keys.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Nav.Down):
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, 1))
	case isBound(k, m.keys.Nav.Up):
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, -1))
	case isBound(k, m.keys.Nav.Top):
		return m.moveReviewCursor(m.review.view.GetFirstRow())
	case isBound(k, m.keys.Nav.Bottom):
		return m.moveReviewCursor(m.review.view.GetLastRow())
	case isBound(k, keys.PageDown):
		return m.pageReview(m.reviewPage())
	case isBound(k, keys.PageUp):
		return m.pageReview(-m.reviewPage())
	case isBound(k, keys.HalfPageDown):
		return m.pageReview(max(1, m.reviewPage()/2))
	case isBound(k, keys.HalfPageUp):
		return m.pageReview(-max(1, m.reviewPage()/2))
	case isBound(k, keys.NextFile):
		return m.jumpReviewFile(1)
	case isBound(k, keys.PrevFile):
		return m.jumpReviewFile(-1)
	case isBound(k, keys.NextHunk):
		return m.moveReviewCursor(m.review.view.JumpHunk(m.review.cursor, 1))
	case isBound(k, keys.PrevHunk):
		return m.moveReviewCursor(m.review.view.JumpHunk(m.review.cursor, -1))
	case isBound(k, keys.NextThread):
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, 1))
	case isBound(k, keys.PrevThread):
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, -1))
	case isBound(k, m.keys.Filter.Open):
		m.review.filter.typing = true

		return m, nil
	case isBound(k, keys.NextMatch):
		return m.jumpReviewMatch(1)
	case isBound(k, keys.PrevMatch):
		return m.jumpReviewMatch(-1)
	case k == recentrePrefix:
		m.review.pending = recentrePrefix

		return m, nil
	case isBound(k, keys.PanRight):
		return m.panReview(m.reviewPanStep())
	case isBound(k, keys.PanLeft):
		return m.panReview(-m.reviewPanStep())
	case isBound(k, keys.PanReset):
		return m.panReview(-m.review.pan)
	case isBound(k, keys.Reload):
		return m.reloadReviewDiff()
	case isBound(k, keys.EditFile):
		return m.editReviewFile()
	case isBound(k, keys.Compose):
		return m.composeReview()
	case isBound(k, keys.Approve):
		return m.approveReview()
	case isBound(k, keys.Resolve):
		return m.resolveReviewThread()
	case isBound(k, keys.ToggleViewed):
		return m.toggleReviewViewed()
	case isBound(k, keys.DeleteThread):
		return m.deleteReviewThread()
	case isBound(k, m.keys.Form.Submit):
		return m.releaseReview()
	}

	return m, nil
}

// refuseReviewWrite says why a key that would write to the agent did nothing, since the key is
// dropped from the help but a rebinding or a habit can still press it.
func (m model) refuseReviewWrite() (tea.Model, tea.Cmd) {
	reason, _ := m.reviewIsReadOnly()
	m.review.notice = reason

	return m, nil
}

// isReviewWrite reports whether a key would put something in front of the agent, which is the
// set that stands down on an agent with nobody there to read it, leaving out settling, deleting
// and marking a file read, since those are the person's own records and send nothing.
func isReviewWrite(k string, keys config.Keybindings) bool {
	return isBound(k, keys.Review.Compose) ||
		isBound(k, keys.Review.Approve) ||
		isBound(k, keys.Form.Submit)
}

// recentrePrefix opens the vim recentring chord, zz, zt and zb, which is left as vim spells it.
const recentrePrefix = "z"

// updateReviewPending answers the key after the recentring prefix, which decides where in the
// pane the cursor's own row is put.
func (m model) updateReviewPending(k string) (tea.Model, tea.Cmd) {
	m.review.pending = ""

	height := m.review.frames.height
	if height <= 0 {
		return m, nil
	}

	switch k {
	case "z":
		return m.putReviewWindow(m.review.cursor - height/2)
	case "t":
		return m.putReviewWindow(m.review.cursor)
	case "b":
		return m.putReviewWindow(m.review.cursor - height + 1)
	}

	return m, nil
}

// putReviewWindow scrolls the diff so the cursor's row lands where a recentring key asked for,
// without moving the cursor itself.
func (m model) putReviewWindow(top int) (tea.Model, tea.Cmd) {
	m.review.top = max(min(top, m.review.view.Len()-1), 0)
	m.review.pinTop = true
	m.stageReview()

	return m, nil
}

// updateReviewFilter is the search prompt taking keys, which is the panel's filter input drawn
// in the review's own sidebar.
func (m model) updateReviewFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	if !m.review.filter.takeKey(key) {
		return m, nil
	}

	m.searchReview()

	return m, nil
}

// searchReview runs the pattern over the diff and moves to the first thing it finds, which is
// what makes typing one worth watching.
func (m *model) searchReview() {
	if m.review.view == nil {
		return
	}

	found := m.review.view.SetPattern(m.review.filter.pattern)
	if found > 0 {
		if next, ok := m.review.view.JumpMatch(m.review.cursor-1, 1); ok {
			m.review.cursor = next
			m.review.pinTop = false
		}
	}

	m.stageReview()
}

// jumpReviewMatch steps to the next or previous line the search found, wrapping at the ends.
func (m model) jumpReviewMatch(direction int) (tea.Model, tea.Cmd) {
	next, ok := m.review.view.JumpMatch(m.review.cursor, direction)
	if !ok {
		m.review.notice = "nothing to search for yet"
		if m.review.filter.isStanding() {
			m.review.notice = "no matches"
		}

		return m, nil
	}

	return m.moveReviewCursor(next)
}

// reviewThreadAtCursor is the thread the cursor is standing on, and false while the diff is
// still loading, since the sidebar is drawn on frames that arrive before the patch does.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.GetThreadAt(m.review.cursor)
}

// reloadReviewDiff opens the next round, which is the only way what is on screen changes, since
// the round the reader is standing in never moves under a comment being written.
func (m model) reloadReviewDiff() (tea.Model, tea.Cmd) {
	return m.reloadReview()
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
	m.review = reviewState{}

	return m.returnToAgents()
}

// jumpReviewFile moves to the next or previous file and puts that file's header at the top of the
// pane, since the scroll margin would otherwise land a forward jump on the last rows of the window
// and show almost nothing of the file it went to.
func (m model) jumpReviewFile(direction int) (tea.Model, tea.Cmd) {
	next := m.review.view.JumpFile(m.review.cursor, direction)
	if next == m.review.cursor {
		return m, nil
	}

	m.review.cursor = next
	m.review.top = max(min(m.review.view.GetFileTopRow(next), m.review.view.Len()-1), 0)
	m.review.pinTop = true
	m.review.pinFloor = m.review.top
	m.stageReview()

	return m, m.markReviewThreadReadCmd()
}

func (m model) moveReviewCursor(next int) (tea.Model, tea.Cmd) {
	if next == m.review.cursor {
		return m, nil
	}

	m.review.cursor = next
	m.review.pinTop = false
	m.stageReview()

	return m, m.markReviewThreadReadCmd()
}

// reviewScrollMargin is how close to the edge of the pane the cursor gets before the diff
// scrolls under it, so there is always some of what is coming into view.
const reviewScrollMargin = 4

// scrollReviewToCursor moves the window only when the cursor is about to leave it, since
// scrolling on every move repaints every row of the pane and reads as a flicker.
func (m *model) scrollReviewToCursor() {
	height := m.review.frames.height
	if height <= 0 {
		return
	}

	// A recentring key has already said where the window goes, and the margin below would drag it
	// straight back off what it was asked to show.
	if m.review.pinTop {
		return
	}

	margin := min(reviewScrollMargin, max((height-1)/2, 0))

	// A file jump is only worth anything if the file stays at the top, so the margin gives up its
	// gap until the cursor walks back above the row the jump landed on.
	floor := 0
	if m.review.cursor >= m.review.pinFloor {
		floor = m.review.pinFloor
	}

	if low := m.review.cursor - margin; low < m.review.top {
		m.review.top = max(low, floor)
	}

	if high := m.review.cursor - height + 1 + margin; high > m.review.top {
		m.review.top = high
	}
}

// markReviewThreadReadCmd claims the thread the cursor has just landed on and only that one,
// since marking the whole list would claim threads were read that were never opened.
func (m model) markReviewThreadReadCmd() tea.Cmd {
	thread, ok := m.reviewThreadAtCursor()
	if !ok || !thread.GetUnreadByHuman() {
		return nil
	}

	agents, agentID, id := m.api.agents, m.review.agentID, thread.GetId()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		_, err := agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: id}))
		if err != nil {
			return reviewWroteMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewWroteMsg{
			agentID: agentID,
			threads: threads,
			err:     err,
		}
	}
}

// reviewPanStep is how far sideways a press goes, a third of the pane, since a fixed few
// characters meant forty presses to cross a wide diff.
func (m model) reviewPanStep() int {
	return max(m.review.frames.width/3, reviewPanStep)
}

func (m model) pageReview(delta int) (tea.Model, tea.Cmd) {
	return m.moveReviewCursor(m.review.view.Page(m.review.cursor, delta))
}

// reviewPage is how many rows a page is, which is the pane the diff is drawn into once that has
// been measured, and the panel's own height until then, since the stage is its sibling in the
// same split and as tall as it.
func (m model) reviewPage() int {
	if height := m.review.frames.height; height > 0 {
		return height
	}

	return max(1, m.height-1)
}

func (m model) panReview(delta int) (tea.Model, tea.Cmd) {
	next := max(0, m.review.pan+delta)
	if next == m.review.pan {
		return m, nil
	}

	m.review.pan = next
	m.stageReview()

	return m, nil
}

// composeReview opens the editor on the line under the cursor, replying in the thread already
// there or opening a new one.
func (m model) composeReview() (tea.Model, tea.Cmd) {
	thread, replying := m.reviewThreadAtCursor()

	// A thread whose code is gone has no line to anchor a new thread to, but replying to it is
	// what it is still on screen for.
	if _, ok := m.review.view.GetAnchor(m.review.cursor); !ok && !replying {
		return m, nil
	}

	m.review.editing = ""
	if replying {
		m.review.editing = thread.GetId()
	}

	m.review.composing = true
	m.review.editor.SetValue("")
	m.review.editor.SetWidth(max(20, m.width-4))

	return m, m.review.editor.Focus()
}

func (m model) updateReviewComposer(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// Every other key is a character being typed into the message, so the two that put the
	// editor away are fixed rather than configured.
	switch {
	case k == "esc":
		m.review.editor.Blur()
		m.review.composing = false

		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
	case isBound(k, m.keys.Form.Submit):
		return m.saveReviewMessage()
	}

	editor, cmd := m.review.editor.Update(key)
	m.review.editor = editor

	return m, cmd
}

func (m model) saveReviewMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.review.editor.Value())

	m.review.editor.Blur()
	m.review.composing = false

	if body == "" {
		return m, nil
	}

	agents, agentID, threadID := m.api.agents, m.review.agentID, m.review.editing
	if threadID != "" {
		// Nothing is said back: the reply is drawn in the thread it went into, which is already on
		// the screen the moment it lands.
		return m, m.reviewWriteCmd("", func(ctx context.Context) error {
			_, err := agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
				ThreadId: threadID,
				Body:     body,
			}))

			return err
		})
	}

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	if !ok {
		return m, nil
	}

	return m, m.reviewWriteCmd("thread opened", func(ctx context.Context) error {
		_, err := agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
			AgentId:     agentID,
			FilePath:    anchor.FilePath,
			OldStart:    int32(anchor.OldStart),
			OldEnd:      int32(anchor.OldEnd),
			NewStart:    int32(anchor.NewStart),
			NewEnd:      int32(anchor.NewEnd),
			QuotedLines: anchor.QuotedLines,
			Body:        body,
		}))

		return err
	})
}

// resolveReviewThread settles the thread the cursor is standing on, or reopens one already
// settled.
func (m model) resolveReviewThread() (tea.Model, tea.Cmd) {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return m, nil
	}

	agents, id, settling := m.api.agents, thread.GetId(), thread.GetResolvedAt() == ""

	notice := "thread reopened"
	if settling {
		notice = "thread settled"
	}

	return m, m.reviewWriteCmd(notice, func(ctx context.Context) error {
		_, err := agents.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{
			Id:       id,
			Resolved: settling,
		}))

		return err
	})
}

// toggleReviewViewed folds the file the cursor is in away once it has been read, or opens it
// again, stepping on to the next file when it has just been folded, since reviewing is a walk
// down them.
func (m model) toggleReviewViewed() (tea.Model, tea.Cmd) {
	file, ok := m.review.view.GetFileAt(m.review.cursor)
	if !ok {
		return m, nil
	}

	path := file.Path()
	viewed := !m.review.view.IsViewed(path)

	// Where the cursor is standing is taken before the rows are built again, since folding the
	// file away takes the line it was on out of the diff.
	place, located := m.review.view.GetPlace(m.review.cursor)
	m.review.view.SetFileViewed(path, viewed)
	m.keepReviewCursor(place, located)

	notice := "file opened again"
	if viewed {
		notice = "file viewed"
		m.review.cursor = m.review.view.JumpFile(m.review.cursor, 1)
		m.review.pinTop = false
	}

	m.stageReview()

	agents, roundID := m.api.agents, m.review.round.GetId()

	return m, m.reviewWriteCmd(notice, func(ctx context.Context) error {
		_, err := agents.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
			RoundId:  roundID,
			FilePath: path,
			Viewed:   viewed,
		}))

		return err
	})
}

func (m model) deleteReviewThread() (tea.Model, tea.Cmd) {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return m, nil
	}

	agents, id := m.api.agents, thread.GetId()

	return m, m.reviewWriteCmd("thread deleted", func(ctx context.Context) error {
		_, err := agents.DeleteReviewThread(ctx, connect.NewRequest(&agentv1.DeleteReviewThreadRequest{Id: id}))

		return err
	})
}

// releaseReview lets go of the comments written and not yet sent, asking the agent for changes,
// which reaches it as one file and one piece of mail rather than a comment at a time.
func (m model) releaseReview() (tea.Model, tea.Cmd) {
	return m.releaseReviewWith(agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED)
}

// approveReview lets the batch go saying the round is good as it stands, which is what tells
// the agent it is done and what gates opening a pull request.
func (m model) approveReview() (tea.Model, tea.Cmd) {
	return m.releaseReviewWith(agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED)
}

func (m model) releaseReviewWith(verdict agentv1.ReviewVerdict) (tea.Model, tea.Cmd) {
	agents, agentID := m.api.agents, m.review.agentID

	return m, m.reviewWriteNoticeCmd(func(ctx context.Context) (string, error) {
		resp, err := agents.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{
			AgentId: agentID,
			Verdict: verdict,
		}))
		if err != nil {
			return "", err
		}

		return releasedNotice(verdict, resp.Msg.GetComments()), nil
	})
}

// releasedNotice says what went, since a review leaving with nothing said about it reads as the
// key not working.
func releasedNotice(verdict agentv1.ReviewVerdict, count int32) string {
	if verdict == agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED {
		return "approved, " + comments(count) + " sent"
	}

	return "changes requested, " + comments(count) + " sent"
}

func comments(count int32) string {
	if count == 1 {
		return "1 comment"
	}

	return fmt.Sprintf("%d comments", count)
}

// reviewWriteCmd runs one write and reads the review back in the same breath.
func (m model) reviewWriteCmd(notice string, write func(context.Context) error) tea.Cmd {
	return m.reviewWriteNoticeCmd(func(ctx context.Context) (string, error) {
		return notice, write(ctx)
	})
}

// reviewWriteNoticeCmd is the same for a write whose notice is only known once it has run,
// since what ringing again did is in the answer rather than in the asking.
func (m model) reviewWriteNoticeCmd(write func(context.Context) (string, error)) tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		notice, err := write(ctx)
		if err != nil {
			return reviewWroteMsg{agentID: agentID, err: err}
		}

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewWroteMsg{
			agentID: agentID,
			notice:  notice,
			threads: threads,
			err:     err,
		}
	}
}

func (m model) worktreeFor(agent agentstore.Agent) workflowstore.WorktreeConfig {
	for _, workflow := range m.listing.workflows {
		if workflow.ID == agent.WorkflowID {
			return workflow.Worktree
		}
	}

	return workflowstore.WorktreeConfig{}
}

// reviewDiffPollInterval is how often the worktree behind the diff is read again while the
// screen is up, slower than the tick because git is being run and nothing waits on the answer:
// the reader is told the diff has moved and reloads when they are ready.
const reviewDiffPollInterval = 5 * time.Second

// reviewRefreshedMsg is the conversation as a tick found it, which is only drawn when the
// probe says something has been said since the sidebar was written.
type reviewRefreshedMsg struct {
	agentID string
	threads []*agentv1.ReviewThread
	err     error
}

// reviewDiffPolledMsg is the worktree as a background read found it, which is compared with
// what is on screen rather than drawn over it.
type reviewDiffPolledMsg struct {
	agentID string
	// moved says the agent has written since the round on screen was taken.
	moved bool
	err   error
}

// refreshReviewCmd is what the panel's own tick asks for while the review is up: the
// conversation every time, and the worktree every few seconds.
func (m *model) refreshReviewCmd(now time.Time) tea.Cmd {
	if m.screen != screenReview || m.review.view == nil {
		return nil
	}

	// A reader mid-input is left alone, since the cursor, the window and the composer are theirs
	// until the key that closes them, and the tick after that picks the work up.
	if m.review.doesTakeEveryKey() {
		return nil
	}

	var cmds []tea.Cmd

	if !m.review.polling {
		m.review.polling = true
		cmds = append(cmds, m.refreshReviewThreadsCmd())
	}

	if !m.review.diffPolling && now.Sub(m.review.diffPolled) >= reviewDiffPollInterval {
		m.review.diffPolling = true
		m.review.diffPolled = now
		cmds = append(cmds, m.pollReviewDiffCmd())
	}

	return tea.Batch(cmds...)
}

func (m model) refreshReviewThreadsCmd() tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		threads, err := listReviewThreads(ctx, agents, agentID)

		return reviewRefreshedMsg{
			agentID: agentID,
			threads: threads,
			err:     err,
		}
	}
}

// pollReviewDiffCmd asks whether the agent has written anything since the round on screen was
// taken, hashing the worktree rather than reading a second patch and leaving the round alone
// either way, since the point of a round is that it does not move under the reader.
func (m model) pollReviewDiffCmd() tea.Cmd {
	agentID, dir := m.review.agentID, m.review.dir
	snapshot := m.review.round.GetSnapshotSha()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		probe, err := diff.ProbeTree(ctx, dir)
		if err != nil {
			return reviewDiffPolledMsg{agentID: agentID, err: err}
		}

		held, err := diff.TreeOf(ctx, dir, snapshot)
		if err != nil {
			return reviewDiffPolledMsg{agentID: agentID, err: err}
		}

		return reviewDiffPolledMsg{agentID: agentID, moved: probe != held}
	}
}

func (m *model) applyReviewRefreshed(msg reviewRefreshedMsg) {
	if msg.agentID != m.review.agentID {
		return
	}

	m.review.polling = false

	// A read that failed says nothing, since the banner belongs to the listing, the screen is
	// still showing the conversation as it last stood, and the next tick asks again.
	if msg.err != nil || m.review.view == nil {
		return
	}

	// Input can have started while the read was in flight, and the probe is left where it was
	// so the tick after the reader is done finds this again.
	if m.review.doesTakeEveryKey() {
		return
	}

	if reviewActivityOf(msg.threads) == m.review.activity {
		return
	}

	m.applyReviewThreads(msg.threads)
}

func (m *model) applyReviewDiffPolled(msg reviewDiffPolledMsg) {
	if msg.agentID != m.review.agentID {
		return
	}

	m.review.diffPolling = false

	if msg.err != nil {
		return
	}

	// A worktree edited back to the round it was cut from is not a move, so the notice goes
	// away rather than standing until the reader opens a round for nothing.
	m.review.moved = msg.moved
}
