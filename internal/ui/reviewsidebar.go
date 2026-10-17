package ui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// sideKind is what a row of the sidebar over the file tree stands for.
type sideKind int

const (
	sideHeading sideKind = iota
	sideBlank
	sideTask
	sideThisTask
	sideRound
	sideRoundsMore
	sideWorktree
	sideForge
	sideNeed
	sideWaiting
	sideDescription
	sideDescriptionLine
)

// isStop reports whether the cursor rests on a row of the kind.
func (k sideKind) isStop() bool {
	switch k {
	case sideTask, sideRound, sideRoundsMore, sideNeed, sideDescription, sideDescriptionLine:
		return true
	case sideHeading, sideBlank, sideThisTask, sideWorktree, sideForge, sideWaiting:
	}

	return false
}

// sideCursor is the row over the file tree the sidebar's cursor rests on.
type sideCursor struct {
	kind sideKind
	id   string
}

func (c sideCursor) key() string {
	return strconv.Itoa(int(c.kind)) + ":" + c.id
}

func parseSideKey(key string) (sideCursor, bool) {
	kind, id, ok := strings.Cut(key, ":")
	if !ok {
		return sideCursor{}, false
	}

	at, err := strconv.Atoi(kind)
	if err != nil {
		return sideCursor{}, false
	}

	return sideCursor{kind: sideKind(at), id: id}, true
}

// sideRow is one row of the sidebar over the file tree.
type sideRow struct {
	sideCursor
	text string
	// depth is how far a task of the stack sits under the task at its root.
	depth int
	// line is the round's mark on the line drawn down the range on screen.
	line string
	// note is said after the row, muted.
	note string
}

// compareState is what komrad said of the work between two rounds.
type compareState int

const (
	comparePending compareState = iota
	compareReadable
	compareUnreadable
)

// sideRows is every row over the file tree, the stack first, then the rounds, the forge, what
// waits on the person and the description.
func (r reviewScreen) sideRows(m *model) []sideRow {
	if r.diff.View() == nil {
		return nil
	}

	sections := [][]sideRow{
		r.stackRows(m),
		r.roundRows(m),
		r.forgeRows(m),
		r.needRows(m),
		r.descriptionRows(m),
	}

	var rows []sideRow
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}

		rows = append(rows, section...)
		rows = append(rows, sideRow{sideCursor: sideCursor{kind: sideBlank}})
	}

	return rows
}

// sideStops is the rows of the sidebar the cursor rests on.
func (r reviewScreen) sideStops(m *model) []sideCursor {
	var stops []sideCursor
	for _, row := range r.sideRows(m) {
		if row.kind.isStop() {
			stops = append(stops, row.sideCursor)
		}
	}

	return stops
}

// sideAt is where the sidebar's cursor sits among the stops, -1 while it is on the file tree.
func (r reviewScreen) sideAt(stops []sideCursor) int {
	if r.side == nil {
		return -1
	}

	return slices.Index(stops, *r.side)
}

func (r reviewScreen) task(m *model) panel.Task {
	return m.listing.tasks[r.taskID]
}

// stackRows is the tasks this one stands on, itself, and the tasks standing on it.
func (r reviewScreen) stackRows(m *model) []sideRow {
	task := r.task(m)
	parents := r.findParents(m, task)
	children := r.findChildren(m, task.ID)
	if len(parents) == 0 && len(children) == 0 {
		return nil
	}

	rows := []sideRow{{sideCursor: sideCursor{kind: sideHeading}, text: "stack"}}
	for depth, parent := range parents {
		rows = append(rows, stackRow(parent, sideTask, depth))
	}

	rows = append(rows, stackRow(task, sideThisTask, len(parents)))
	rows = r.appendDescendants(m, rows, task.ID, len(parents)+1)

	return rows
}

// appendDescendants adds the tasks standing on a task and on each of those in turn, each under its parent.
func (r reviewScreen) appendDescendants(m *model, rows []sideRow, id string, depth int) []sideRow {
	for _, child := range r.findChildren(m, id) {
		if slices.ContainsFunc(rows, func(row sideRow) bool { return row.id == child.ID }) {
			continue
		}

		rows = append(rows, stackRow(child, sideTask, depth))
		rows = r.appendDescendants(m, rows, child.ID, depth+1)
	}

	return rows
}

func stackRow(task panel.Task, kind sideKind, depth int) sideRow {
	row := sideRow{
		sideCursor: sideCursor{kind: kind, id: task.ID},
		text:       task.Slug,
		depth:      depth,
	}

	if task.PullRequest.Number > 0 {
		row.note = task.PullRequest.Ref()
	}
	if blocked := task.Stand.GetBlocked(); blocked != "" {
		row.note = strings.TrimSpace(row.note + " " + blocked)
	}

	return row
}

// findParents is the tasks a task stands on, the one at the root first.
func (r reviewScreen) findParents(m *model, task panel.Task) []panel.Task {
	var parents []panel.Task
	seen := map[string]bool{task.ID: true}
	for id := task.Stand.GetTaskId(); id != "" && !seen[id]; {
		parent, ok := m.listing.tasks[id]
		if !ok {
			break
		}

		seen[id] = true
		parents = append(parents, parent)
		id = parent.Stand.GetTaskId()
	}

	slices.Reverse(parents)

	return parents
}

// findChildren is the tasks standing on a task, oldest first.
func (r reviewScreen) findChildren(m *model, id string) []panel.Task {
	var children []panel.Task
	for _, task := range m.listing.tasks {
		if task.Stand.GetTaskId() == id && task.ArchivedAt == nil {
			children = append(children, task)
		}
	}

	slices.SortFunc(children, func(a, b panel.Task) int { return a.CreatedAt.Compare(b.CreatedAt) })

	return children
}

// roundRows is where the task starts, the rounds worth naming, the rest folded between them, and
// the worktree once it has moved past the newest round.
func (r reviewScreen) roundRows(m *model) []sideRow {
	d := r.diff
	if d.newest == nil {
		return nil
	}

	rows := []sideRow{
		{sideCursor: sideCursor{kind: sideHeading}, text: "rounds"},
		{sideCursor: sideCursor{kind: sideRound}, text: taskStartSaid, line: d.rangeLine(0, 0)},
	}

	kept := d.keptRounds()
	var folded []*agentv1.ReviewRound
	fold := func() {
		if len(folded) == 0 {
			return
		}

		rows = append(rows, sideRow{
			sideCursor: sideCursor{kind: sideRoundsMore, id: folded[0].GetId()},
			text:       fmt.Sprintf("%d more", len(folded)),
			line:       d.rangeLine(folded[0].GetOrdinal(), folded[len(folded)-1].GetOrdinal()),
		})
		folded = nil
	}

	for _, round := range d.rounds {
		if !r.openRounds && !kept[round.GetId()] {
			folded = append(folded, round)

			continue
		}

		fold()
		rows = append(rows, sideRow{
			sideCursor: sideCursor{kind: sideRound, id: round.GetId()},
			text:       fmt.Sprintf("round %d", round.GetOrdinal()),
			line:       d.rangeLine(round.GetOrdinal(), round.GetOrdinal()),
			note:       r.reviewersSaid(m, round.GetId()),
		})
	}
	fold()

	if r.task(m).MovedSinceRound != "" {
		rows = append(rows, sideRow{sideCursor: sideCursor{kind: sideWorktree}, text: "the worktree, not yet a round"})
	}

	return rows
}

// keptRounds is the rounds named in the list: those with a released review, the newest, and the
// ends of the range on screen.
func (d reviewDiff) keptRounds() map[string]bool {
	kept := map[string]bool{d.newest.GetId(): true}
	for _, review := range d.reviews {
		if review.GetReleasedAt() != "" {
			kept[review.GetRoundId()] = true
		}
	}

	if d.spec.isRange() {
		kept[d.spec.from] = true
		kept[d.endRound().GetId()] = true
	}

	return kept
}

// rangeLine is the mark a run of rounds carries on the line drawn down the range on screen.
func (d reviewDiff) rangeLine(first, last int32) string {
	if !d.spec.isRange() {
		return " "
	}

	start, end := d.ordinalOf(d.spec.from), d.endRound().GetOrdinal()
	if d.spec.from == "" {
		start = 0
	}

	switch {
	case first == start && last == start:
		return "┬"
	case first == end && last == end:
		return "┴"
	case first > start && last < end:
		return "│"
	}

	return " "
}

// reviewersSaid names who reviewed a round and what each concluded.
func (r reviewScreen) reviewersSaid(m *model, roundID string) string {
	var said []string
	for _, review := range r.diff.reviews {
		if review.GetRoundId() != roundID || review.GetReleasedAt() == "" {
			continue
		}

		said = append(said, r.reviewerName(m, review)+" "+verdictMark(m, review.GetVerdict()))
	}

	return strings.Join(said, "  ")
}

func (r reviewScreen) reviewerName(m *model, review *agentv1.Review) string {
	switch {
	case review.GetExternalLogin() != "":
		return "@" + review.GetExternalLogin()
	case review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT:
		return "you"
	}

	if item, ok := m.listing.anyAgent(review.GetAuthorAgentId()); ok {
		return item.Agent.Name
	}

	return "reviewer"
}

// forgeRows is what the pull request's forge says of it: its review decision and who commented.
func (r reviewScreen) forgeRows(m *model) []sideRow {
	task := r.task(m)
	if task.PullRequest.Number == 0 {
		return nil
	}

	rows := []sideRow{{sideCursor: sideCursor{kind: sideHeading}, text: r.forgeName(m)}}
	if said := decisionSaid(task.PullRequest.MergeReadiness.Decision); said != "" {
		rows = append(rows, sideRow{sideCursor: sideCursor{kind: sideForge, id: "decision"}, text: said})
	}

	counts := map[string]int{}
	var logins []string
	for _, thread := range r.threads.list {
		if !thread.GetMirrorsForge() || len(thread.GetMessages()) == 0 {
			continue
		}

		login := describeForgeLogin(thread.GetMessages()[0].GetForge().GetLogin())
		if counts[login] == 0 {
			logins = append(logins, login)
		}
		counts[login]++
	}

	for _, login := range logins {
		rows = append(rows, sideRow{
			sideCursor: sideCursor{kind: sideForge, id: login},
			text:       login,
			note:       countedThreads(counts[login]),
		})
	}

	if len(rows) == 1 {
		return nil
	}

	return rows
}

func countedThreads(n int) string {
	if n == 1 {
		return "1 thread"
	}

	return fmt.Sprintf("%d threads", n)
}

// needRows is what waits on the person: replies they have not read, forge comments to pass on,
// drafts not sent, and a count of their threads waiting on the writer.
func (r reviewScreen) needRows(m *model) []sideRow {
	var rows []sideRow
	waiting := 0
	for _, thread := range r.threads.list {
		if isPersonThread(thread) && thread.GetUnreadByHuman() {
			rows = append(rows, r.needRow(thread, "reply on", ""))

			continue
		}

		if thread.GetResolvedAt() != "" {
			continue
		}

		switch {
		case isHeldThread(thread):
			rows = append(rows, r.needRow(thread, "forge comment on", ""))
		case holdsDraft(thread):
			note := ""
			if r.diff.View().IsGhosted(thread) {
				note = "the code under this has changed"
			}
			rows = append(rows, r.needRow(thread, "draft on", note))
		case isPersonThread(thread) && isWaitingOnWriter(thread):
			waiting++
		}
	}

	if waiting > 0 {
		rows = append(rows, sideRow{
			sideCursor: sideCursor{kind: sideWaiting},
			text:       fmt.Sprintf("%s of yours waiting on the writer", countedThreads(waiting)),
		})
	}

	if len(rows) == 0 {
		return nil
	}

	return append([]sideRow{{sideCursor: sideCursor{kind: sideHeading}, text: "needs you"}}, rows...)
}

func (r reviewScreen) needRow(thread *agentv1.ReviewThread, said, note string) sideRow {
	return sideRow{
		sideCursor: sideCursor{kind: sideNeed, id: thread.GetId()},
		text:       fmt.Sprintf("%s %s:%d", said, thread.GetFilePath(), thread.GetNewStart()),
		note:       note,
	}
}

func isPersonThread(thread *agentv1.ReviewThread) bool {
	said := thread.GetMessages()

	return len(said) > 0 && said[0].GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
}

func isHeldThread(thread *agentv1.ReviewThread) bool {
	_, held := heldForgeTarget(thread)

	return held
}

func holdsDraft(thread *agentv1.ReviewThread) bool {
	return slices.ContainsFunc(thread.GetMessages(), func(message *agentv1.ReviewMessage) bool {
		return message.GetDelivery() == agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT
	})
}

// isWaitingOnWriter reports whether the person spoke last on a thread and sent it.
func isWaitingOnWriter(thread *agentv1.ReviewThread) bool {
	said := thread.GetMessages()
	last := said[len(said)-1]

	return last.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN &&
		last.GetDelivery() != agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT
}

// descriptionRows is the pull request's title, and its description once opened.
func (r reviewScreen) descriptionRows(m *model) []sideRow {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return nil
	}

	rows := []sideRow{{
		sideCursor: sideCursor{kind: sideDescription},
		text:       pullRequest.Ref() + " " + pullRequest.Title,
	}}
	if !r.openDescription {
		return rows
	}

	if pullRequest.Author != "" {
		rows = append(rows, sideRow{sideCursor: sideCursor{kind: sideDescriptionLine, id: "author"}, text: "@" + pullRequest.Author})
	}

	for at, line := range pullRequestBodyRows(pullRequest.Body, lineWidthOf(m.paneWidth())-2) {
		rows = append(rows, sideRow{sideCursor: sideCursor{kind: sideDescriptionLine, id: strconv.Itoa(at)}, text: line})
	}

	return rows
}

// moveSide walks the rows over the file tree, false while the move stays on the tree.
func (r *reviewScreen) moveSide(m *model, rows int) (tea.Cmd, bool) {
	stops := r.sideStops(m)
	tree := r.readFilesTree()
	at := r.sideAt(stops)
	if at < 0 {
		treeAt := max(tree.current, 0)
		if rows >= 0 || len(stops) == 0 || treeAt+rows >= 0 {
			return nil, false
		}

		at = len(stops) + treeAt
	}

	next := max(at+rows, 0)
	if next >= len(stops) && len(tree.rows) > 0 {
		r.side = nil

		return r.landOnRow(m, tree.rows[min(next-len(stops), len(tree.rows)-1)]), true
	}

	return r.restOnSide(m, stops[min(next, len(stops)-1)]), true
}

// moveSideToTop puts the cursor on the first row over the file tree, false while the cursor is on the tree.
func (r *reviewScreen) moveSideToTop(m *model) (tea.Cmd, bool) {
	stops := r.sideStops(m)
	if r.side == nil || len(stops) == 0 {
		return nil, false
	}

	return r.restOnSide(m, stops[0]), true
}

// restOnSide puts the cursor on a row over the file tree, asking komrad about the ranges a round
// would set.
func (r *reviewScreen) restOnSide(m *model, cursor sideCursor) tea.Cmd {
	r.side = &cursor
	r.folderCursor = folderCursor{}

	if cursor.kind != sideRound || cursor.id == "" {
		return nil
	}

	return r.compareFor(m, cursor.id)
}

// startPair is the range B on a round would set.
func (d reviewDiff) startPair(id string) reviewStep {
	to := d.rangeEnd()
	if d.ordinalOf(id) >= d.ordinalOf(to) {
		to = ""
	}

	return reviewStep{from: id, to: cmp.Or(to, d.newest.GetId())}
}

// onlyPair is the range O on a round would set.
func (d reviewDiff) onlyPair(id string) reviewStep {
	return reviewStep{from: d.roundBefore(id), to: id}
}

// compareFor asks komrad whether the ranges B and O on a round would set can be read.
func (r *reviewScreen) compareFor(m *model, id string) tea.Cmd {
	if r.compared == nil {
		r.compared = map[reviewStep]compareState{}
	}

	var cmds []tea.Cmd
	for _, pair := range []reviewStep{r.diff.startPair(id), r.diff.onlyPair(id)} {
		if pair.from == "" || pair.to == "" || pair.from == pair.to {
			continue
		}
		if _, asked := r.compared[pair]; asked {
			continue
		}

		r.compared[pair] = comparePending
		cmds = append(cmds, compareRoundsCmd(m, r.agentID, r.taskID, pair))
	}

	return tea.Batch(cmds...)
}

// reviewComparedMsg is what komrad said of the work between two rounds.
type reviewComparedMsg struct {
	agentID    string
	pair       reviewStep
	comparable bool
	err        error
}

func compareRoundsCmd(m *model, agentID, taskID string, pair reviewStep) tea.Cmd {
	agents := m.api.reads().agents

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		resp, err := agents.CompareReviewRounds(ctx, connect.NewRequest(&agentv1.CompareReviewRoundsRequest{
			Task:        taskID,
			FromRoundId: pair.from,
			ToRoundId:   pair.to,
		}))
		if err != nil {
			return reviewComparedMsg{agentID: agentID, pair: pair, err: err}
		}

		return reviewComparedMsg{agentID: agentID, pair: pair, comparable: resp.Msg.GetComparable()}
	}
}

func (r *reviewScreen) applyCompared(msg reviewComparedMsg) {
	if msg.agentID != r.agentID || r.compared == nil {
		return
	}

	switch {
	case msg.err != nil:
		delete(r.compared, msg.pair)
	case msg.comparable:
		r.compared[msg.pair] = compareReadable
	default:
		r.compared[msg.pair] = compareUnreadable
	}
}

// unreadableSaid is why a range is drawn off, empty while komrad has not said it cannot be read.
func (r reviewScreen) unreadableSaid(pair reviewStep) string {
	if r.compared[pair] != compareUnreadable || pair.from == "" {
		return ""
	}

	return fmt.Sprintf("komrad cannot read the work from round %d to round %d", r.diff.ordinalOf(pair.from), r.diff.ordinalOf(pair.to))
}

// chooseSide does what the row under the sidebar's cursor is for, false when it is for nothing.
func (r *reviewScreen) chooseSide(m *model) (tea.Cmd, bool) {
	if r.side == nil {
		return nil, false
	}

	switch r.side.kind {
	case sideRoundsMore:
		r.openRounds = true

		return nil, true
	case sideDescription:
		r.openDescription = !r.openDescription

		return nil, true
	case sideNeed:
		return r.landOnThread(m, r.side.id), true
	case sideTask:
		return r.openStackedReview(m, r.side.id), true
	}

	return nil, false
}

// chooseDesc says what choosing the row under the sidebar's cursor does.
func (r reviewScreen) chooseDesc() string {
	if r.side == nil {
		return r.folderDesc()
	}

	switch r.side.kind {
	case sideRoundsMore:
		return "show every round"
	case sideDescription:
		if r.openDescription {
			return "close the description"
		}

		return "open the description"
	case sideNeed:
		return "go to the comment"
	case sideTask:
		return "open its review"
	}

	return ""
}

// doesChoose reports whether the open key does something where the sidebar's cursor rests.
func (r reviewScreen) doesChoose() bool {
	if r.side == nil {
		_, ok := r.readCurrentFolder()

		return ok
	}

	return r.chooseDesc() != ""
}

// landOnThread puts the diff's cursor on a thread, widening the diff when the range on screen
// leaves it out.
func (r *reviewScreen) landOnThread(m *model, id string) tea.Cmd {
	r.blurBlock()
	if y, ok := r.diff.View().GetThreadRow(id); ok && slices.ContainsFunc(r.diff.threadsOnScreen(r.threads.list), func(t *agentv1.ReviewThread) bool { return t.GetId() == id }) {
		return r.moveCursor(m, y)
	}

	r.diff.widen()
	r.diff.hidesAgentThreads = false
	r.diff.landOn = id

	return r.reload(m)
}

// openStackedReview leaves this review for the review of another task in the stack.
func (r *reviewScreen) openStackedReview(m *model, taskID string) tea.Cmd {
	if said := r.stackedOffSaid(m, taskID); said != "" {
		m.note(messageWarning, said)

		return nil
	}

	writer, _ := m.listing.findTaskWriter(m.listing.tasks[taskID])
	letGo := r.endHold(m, abandonedSaid)

	return tea.Batch(letGo, m.enterReview(writer.Agent))
}

// stackedOffSaid is why the review of a task in the stack cannot open, empty when it can.
func (r reviewScreen) stackedOffSaid(m *model, taskID string) string {
	task := m.listing.tasks[taskID]
	writer, ok := m.listing.findTaskWriter(task)
	switch {
	case r.hold.isHeld():
		return "close the editor first"
	case task.WorktreeRemoved || task.Cwd == "":
		return "its worktree is gone"
	case !ok || writer.Agent.ArchivedAt != nil:
		return "its writer is archived"
	case !m.listing.isReviewable(writer.Agent):
		return errReviewNoCheckout.Error()
	}

	return ""
}

// sideMenu is the band on the row under the sidebar's cursor, false for a row with none.
func (r *reviewScreen) sideMenu(m *model) (submenu, bool) {
	switch r.side.kind {
	case sideRound:
		return r.roundMenu(m, r.side.id), true
	case sideTask:
		task := m.listing.tasks[r.side.id]
		review := openEntry("review", m.keys.Menu.Review, func(m *model) tea.Cmd {
			return m.review.openStackedReview(m, task.ID)
		})

		return submenu{
			kind:    formMenu,
			title:   task.Slug,
			entries: []submenuEntry{drawnOff(review, r.stackedOffSaid(m, task.ID))},
		}, true
	}

	return submenu{}, false
}

// roundMenu is the band on a round, which sets the range on screen from it.
func (r *reviewScreen) roundMenu(m *model, id string) submenu {
	d := r.diff
	start := openEntry("start the range here", m.keys.Review.RangeStart, func(m *model) tea.Cmd {
		return m.review.startRangeAt(m, id)
	})

	startSaid := ""
	if d.spec.isRange() && d.rangeStart() == id {
		startSaid = "the range starts here"
	}

	if id == "" {
		return submenu{kind: formMenu, title: taskStartSaid, entries: []submenuEntry{drawnOff(start, startSaid)}}
	}

	switch {
	case startSaid != "":
	case id == d.newest.GetId():
		startSaid = "O shows this round alone"
	default:
		startSaid = r.unreadableSaid(d.startPair(id))
	}

	endSaid := ""
	if d.spec.isRange() && cmp.Or(d.rangeEnd(), d.newest.GetId()) == id {
		endSaid = "the range ends here"
	}

	only := d.onlyPair(id)
	onlySaid := r.unreadableSaid(only)
	if endSaid != "" && d.rangeStart() == only.from {
		onlySaid = "this round alone is on screen"
	}

	end := openEntry("end the range here", m.keys.Review.RangeEnd, func(m *model) tea.Cmd {
		return m.review.endRangeAt(m, id)
	})
	alone := openEntry("show this round alone", m.keys.Review.RangeOnly, func(m *model) tea.Cmd {
		return m.review.narrowTo(m, id)
	})

	return submenu{
		kind:  formMenu,
		title: fmt.Sprintf("round %d", d.ordinalOf(id)),
		entries: []submenuEntry{
			drawnOff(start, startSaid),
			drawnOff(end, endSaid),
			drawnOff(alone, onlySaid),
		},
	}
}

func (r *reviewScreen) clickSide(m *model, key string) tea.Cmd {
	cursor, ok := parseSideKey(key)
	if !ok || !cursor.kind.isStop() {
		return nil
	}

	moved := r.restOnSide(m, cursor)
	chosen, _ := r.chooseSide(m)

	return tea.Batch(moved, chosen)
}
