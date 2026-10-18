package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// sideKind is what a row of the sidebar over the file tree stands for.
type sideKind int

const (
	sideHeading sideKind = iota
	sideBlank
	sideTask
	sideThisTask
	sideForge
	sideNeed
	sideWaiting
	sideDescription
	sideDescriptionLine
)

// isStop reports whether the cursor rests on a row of the kind.
func (k sideKind) isStop() bool {
	switch k {
	case sideTask, sideNeed, sideDescription, sideDescriptionLine:
		return true
	case sideHeading, sideBlank, sideThisTask, sideForge, sideWaiting:
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
	// note is said after the row, muted.
	note string
}

// sideRows is every row over the file tree, the stack first, then the forge, what waits on the
// person and the description.
func (r reviewScreen) sideRows(m *model) []sideRow {
	if r.diff.View() == nil {
		return nil
	}

	sections := [][]sideRow{
		r.stackRows(m),
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

	r.restOnSide(stops[min(next, len(stops)-1)])

	return nil, true
}

// moveSideToTop puts the cursor on the first row over the file tree, false while the cursor is on the tree.
func (r *reviewScreen) moveSideToTop(m *model) (tea.Cmd, bool) {
	stops := r.sideStops(m)
	if r.side == nil || len(stops) == 0 {
		return nil, false
	}

	r.restOnSide(stops[0])

	return nil, true
}

// restOnSide puts the cursor on a row over the file tree.
func (r *reviewScreen) restOnSide(cursor sideCursor) {
	r.side = &cursor
	r.folderCursor = folderCursor{}
}

// chooseSide does what the row under the sidebar's cursor is for, false when it is for nothing.
func (r *reviewScreen) chooseSide(m *model) (tea.Cmd, bool) {
	if r.side == nil {
		return nil, false
	}

	switch r.side.kind {
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

func (r *reviewScreen) clickSide(m *model, key string) tea.Cmd {
	cursor, ok := parseSideKey(key)
	if !ok || !cursor.kind.isStop() {
		return nil
	}

	r.restOnSide(cursor)
	chosen, _ := r.chooseSide(m)

	return chosen
}
