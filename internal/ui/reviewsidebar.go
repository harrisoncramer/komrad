package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
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
func (r reviewScreen) sideRows(m *model, in screen.Inputs) []sideRow {
	if r.diff.View() == nil {
		return nil
	}

	sections := [][]sideRow{
		r.stackRows(m),
		r.forgeRows(m),
		r.needRows(),
		r.descriptionRows(m, in),
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
func (r reviewScreen) sideStops(m *model, in screen.Inputs) []sideCursor {
	var stops []sideCursor
	for _, row := range r.sideRows(m, in) {
		if row.kind.isStop() {
			stops = append(stops, row.sideCursor)
		}
	}

	return stops
}

// findSide is the row over the file tree the cursor rests on, false while it rests on the tree or on a row
// since gone from the sidebar.
func (r reviewScreen) findSide(m *model, in screen.Inputs) (sideCursor, bool) {
	if r.side == nil || !slices.Contains(r.sideStops(m, in), *r.side) {
		return sideCursor{}, false
	}

	return *r.side, true
}

// sideAt is where the sidebar's cursor sits among the stops, -1 while it is on the file tree.
func (r reviewScreen) sideAt(stops []sideCursor) int {
	if r.side == nil {
		return -1
	}

	return slices.Index(stops, *r.side)
}

// findTask is the task under review.
func (r reviewScreen) findTask(m *model) panel.Task {
	return m.listing.Tasks[r.taskID]
}

// stackRows is the task's stack: its ancestors, itself, and its descendants.
func (r reviewScreen) stackRows(m *model) []sideRow {
	task := r.findTask(m)
	tasks := m.listing.Tasks
	ancestors := tasks.AncestorsOf(task)
	children := tasks.ChildrenOf(task.ID)
	if len(ancestors) == 0 && len(children) == 0 {
		return nil
	}

	rows := []sideRow{{sideCursor: sideCursor{kind: sideHeading}, text: "stack"}}
	for depth, ancestor := range ancestors {
		rows = append(rows, stackRow(tasks, ancestor, sideTask, depth))
	}

	rows = append(rows, stackRow(tasks, task, sideThisTask, len(ancestors)))
	rows = appendDescendants(tasks, rows, task.ID, len(ancestors)+1)

	return rows
}

// appendDescendants adds a task's children and their children in turn, each under its parent.
func appendDescendants(tasks panel.Tasks, rows []sideRow, id string, depth int) []sideRow {
	for _, child := range tasks.ChildrenOf(id) {
		if slices.ContainsFunc(rows, func(row sideRow) bool { return row.id == child.ID }) {
			continue
		}

		rows = append(rows, stackRow(tasks, child, sideTask, depth))
		rows = appendDescendants(tasks, rows, child.ID, depth+1)
	}

	return rows
}

// stackRow is one task of the stack, with its pull request and what blocks its cascade said after it.
func stackRow(tasks panel.Tasks, task panel.Task, kind sideKind, depth int) sideRow {
	row := sideRow{
		sideCursor: sideCursor{kind: kind, id: task.ID},
		text:       task.Slug,
		depth:      depth,
	}

	if task.PullRequest.Number > 0 {
		row.note = task.PullRequest.Ref()
	}
	if said := cascadeBlockSaid(task.Parent.GetBlocked(), tasks[task.Parent.GetTaskId()].Slug); said != "" {
		row.note = strings.TrimSpace(row.note + " " + said)
	}

	return row
}

// forgeRows is what the pull request's forge says of it: its review decision and who commented.
func (r reviewScreen) forgeRows(m *model) []sideRow {
	task := r.findTask(m)
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

	rows = append(rows, xslice.Map(logins, func(login string) sideRow {
		return sideRow{
			sideCursor: sideCursor{kind: sideForge, id: login},
			text:       login,
			note:       countedThreads(counts[login]),
		}
	})...)

	if len(rows) == 1 {
		return nil
	}

	return rows
}

// decisionSaid is the forge's review decision on the pull request, empty when it has none.
func decisionSaid(decision pullrequestv1.ReviewDecision) string {
	switch decision {
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED:
		return "approved"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED:
		return "changes requested"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING:
		return "review required"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED:
		return "review requested again"
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_UNSPECIFIED, pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE:
	}

	return ""
}

func countedThreads(count int) string {
	if count == 1 {
		return "1 thread"
	}

	return fmt.Sprintf("%d threads", count)
}

// needRows is what waits on the person: replies they have not read, forge comments to pass on,
// and a count of their threads waiting on the writer.
func (r reviewScreen) needRows() []sideRow {
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
		text:       said + " " + diffview.FormatThreadLocation(thread),
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

// isWaitingOnWriter reports whether the person spoke last on a thread.
func isWaitingOnWriter(thread *agentv1.ReviewThread) bool {
	said := thread.GetMessages()

	return said[len(said)-1].GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
}

// descriptionRows is the pull request's title, and its description once opened.
func (r reviewScreen) descriptionRows(m *model, in screen.Inputs) []sideRow {
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

	width := lineWidthOf(in.Room.Width) - len(sideMarkBlank) - len(descriptionIndent)
	for at, line := range pullRequestBodyRows(pullRequest.Body, width) {
		rows = append(rows, sideRow{sideCursor: sideCursor{kind: sideDescriptionLine, id: strconv.Itoa(at)}, text: line})
	}

	return rows
}

// moveSide walks the rows over the file tree, false while the move stays on the tree.
func (r *reviewScreen) moveSide(m *model, rows int) (screen.Request, bool) {
	stops := r.sideStops(m, m.inputs())
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
func (r *reviewScreen) moveSideToTop(m *model) bool {
	in := m.inputs()
	if _, ok := r.findSide(m, in); !ok {
		return false
	}

	r.restOnSide(r.sideStops(m, in)[0])

	return true
}

// restOnSide puts the cursor on a row over the file tree.
func (r *reviewScreen) restOnSide(cursor sideCursor) {
	r.side = &cursor
	r.folderCursor = folderCursor{}
}

// isSideFocused reports whether the rows over the file tree hold the review's keys.
func (r *reviewScreen) isSideFocused(m *model, in screen.Inputs) bool {
	_, ok := r.findSide(m, in)

	return ok && r.isBlockFocused(m, in)
}

// chooseSide does what the row under the sidebar's cursor is for.
func (r *reviewScreen) chooseSide(m *model, in screen.Inputs) screen.Request {
	side, ok := r.findSide(m, in)
	if !ok {
		return nil
	}

	switch side.kind {
	case sideDescription:
		r.openDescription = !r.openDescription
	case sideNeed:
		return r.landOnThread(m, side.id)
	case sideTask:
		return r.openStackedReview(m, in, side.id)
	case sideHeading, sideBlank, sideThisTask, sideForge, sideWaiting, sideDescriptionLine:
	}

	return nil
}

// chooseDesc says what choosing the row under the sidebar's cursor does, empty when it does nothing.
func (r reviewScreen) chooseDesc(m *model) string {
	side, ok := r.findSide(m, m.inputs())
	if !ok {
		return ""
	}

	switch side.kind {
	case sideDescription:
		if r.openDescription {
			return "close the description"
		}

		return "open the description"
	case sideNeed:
		return "go to the comment"
	case sideTask:
		return "open its review"
	case sideHeading, sideBlank, sideThisTask, sideForge, sideWaiting, sideDescriptionLine:
	}

	return ""
}

// landOnThread puts the diff's cursor on a thread, widening the diff when the range on screen
// leaves it out.
func (r *reviewScreen) landOnThread(m *model, id string) screen.Request {
	r.blurBlock()
	if y, ok := r.diff.View().GetThreadRow(id); ok && r.diff.isThreadOnScreen(r.threads.list, id) {
		return r.moveCursor(m, y)
	}

	r.diff.widen()
	r.diff.hidesAgentThreads = false
	r.diff.landOn = id

	return r.reload()
}

// openStackedReview leaves this review for the review of another task in the stack.
func (r *reviewScreen) openStackedReview(m *model, in screen.Inputs, taskID string) screen.Request {
	if said := r.stackedOffSaid(m, taskID); said != "" {
		return screen.NewNote(screen.Warning, said)
	}

	writer, _ := in.Listing.FindTaskWriter(in.Listing.Tasks[taskID])

	return screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: writer.Agent.ID}}
}

// stackedOffSaid is why the review of a task in the stack cannot open, empty when it can.
func (r reviewScreen) stackedOffSaid(m *model, taskID string) string {
	task := m.listing.Tasks[taskID]
	writer, ok := m.listing.FindTaskWriter(task)
	switch {
	case r.edit.atEditor:
		return "close the editor first"
	case task.IsWorktreeRemoved() || task.Cwd == "":
		return "its worktree is gone"
	case !ok || writer.Agent.ArchivedAt != nil:
		return "its writer is archived"
	case !m.listing.IsReviewable(writer.Agent):
		return errReviewNoCheckout.Error()
	}

	return ""
}

// sideMenu is the band on the row under the sidebar's cursor, false for a row with none.
func (r *reviewScreen) sideMenu(m *model, in screen.Inputs, side sideCursor) (menu.Menu, bool) {
	if side.kind != sideTask {
		return menu.Menu{}, false
	}

	task := in.Listing.Tasks[side.id]
	review := menu.Leaving("review", r.deps.keys.Menu.Review, func() screen.Request {
		return m.review.openStackedReview(m, m.inputs(), task.ID)
	})

	return menu.Menu{
		Title:   task.Slug,
		Entries: []menu.Entry{menu.DrawnOff(review, r.stackedOffSaid(m, task.ID))},
	}, true
}

func (r *reviewScreen) clickSide(m *model, in screen.Inputs, key string) screen.Request {
	cursor, ok := parseSideKey(key)
	if !ok || !cursor.kind.isStop() {
		return nil
	}

	r.restOnSide(cursor)

	return r.chooseSide(m, in)
}
