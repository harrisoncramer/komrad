package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/git"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/view"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// entriesShown is how many of a directory's entries the document lists before counting the rest.
const entriesShown = 10

// enter lands on the directory of the agent the target names, remembering it as the focus for
// the listing on its way.
func (w *worktreesScreen) enter(h host, s panelShared, target screenTarget) tea.Cmd {
	w.focus = target.agentID
	w.apply(s, s.listing.worktrees, w.list().hovered(s))
	w.stage(h, s)

	return h.waitToStage()
}

func (w *worktreesScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return w.list().update(h, s, key, w.actions())
}

func (w *worktreesScreen) list() screenList[worktreeRow] {
	return screenList[worktreeRow]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.rows,
		name:   worktreeRow.name,
		refs:   worktreeRefs,
		header: "repository",
		placed: w.stage,
		moved: func(h host, _ panelShared) tea.Cmd {
			return h.waitToStage()
		},
		quit: func(k config.Keybindings) []string { return k.Worktrees.Quit },
		back: func(k config.Keybindings) []string { return k.Worktrees.Back },
	}
}

func (w *worktreesScreen) hovered(s panelShared) (worktreeRow, bool) {
	row, ok := rowAt(w.rows(s), w.cursor)
	if !ok || row.header {
		return worktreeRow{}, false
	}

	return row, true
}

func (w *worktreesScreen) stage(h host, _ panelShared) {
	h.stageInfo()
}

// inspectWorktreeCmd asks the service for the hovered directory's standing.
func (m *model) inspectWorktreeCmd() tea.Cmd {
	row, ok := m.worktrees.hovered(m.shared())
	if !ok {
		return nil
	}

	return m.readWorktreeCmd(row.item.dir)
}

// readWorktreeCmd asks the service for one directory's standing, once at a time.
func (m *model) readWorktreeCmd(dir string) tea.Cmd {
	last, asked := m.inspections[dir]
	if asked && last.pending {
		return nil
	}

	pending := last
	pending.pending = true
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: dir, inspection: pending})
	clients := m.api

	return func() tea.Msg {
		read, err := clients.getWorktree(dir)
		if err != nil {
			// The last answer stays on the row with the refusal beside it.
			last.answered, last.readAt, last.err = true, time.Now(), err

			return worktreeInspectedMsg{dir: dir, inspection: last}
		}

		return worktreeInspectedMsg{dir: dir, inspection: worktreeInspectionFrom(read, time.Now())}
	}
}

// inspectHoveredWorktreeCmd asks for the hovered directory's standing the first time.
func (m *model) inspectHoveredWorktreeCmd() tea.Cmd {
	if m.screen != screenWorktrees {
		return nil
	}

	row, ok := m.worktrees.hovered(m.shared())
	if !ok {
		return nil
	}
	if _, asked := m.inspections[row.item.dir]; asked {
		return nil
	}

	return m.inspectWorktreeCmd()
}

// worktreeInspectedMsg is one directory's standing come back.
type worktreeInspectedMsg struct {
	dir        string
	inspection worktreeInspection
}

func (m *model) applyWorktreeInspection(msg worktreeInspectedMsg) {
	if m.inspections == nil {
		m.inspections = map[string]worktreeInspection{}
	}

	m.inspections[msg.dir] = msg.inspection
}

// applyWorktrees hands the screen a fresh listing and forgets the readings of directories that
// have left it.
func (m *model) applyWorktrees(items []worktreeItem, hovered string) {
	listedDirs := make(map[string]bool, len(items))
	for _, item := range items {
		listedDirs[item.dir] = true
	}
	for dir := range m.inspections {
		if !listedDirs[dir] {
			delete(m.inspections, dir)
		}
	}

	m.worktrees.apply(m.shared(), items, hovered)
}

func (w *worktreesScreen) view(s panelShared) string {
	w.drawn = w.buildRows(s)
	defer func() { w.drawn = nil }()

	return s.draw.columnListView(w.listParams(s))
}

func worktreesTabs() tabStrip {
	return tabStrip{
		title:    "f",
		legend:   "sort",
		options:  tabOptionsOf(worktreeSorts),
		fallback: "size",
		grouped:  "repo",
	}
}

// tally is how many directories the listing holds, what the measured ones take, and whether
// they are drawn as one list.
func (w *worktreesScreen) tally() string {
	tally := fmt.Sprintf("%d"+separated+"%s", len(w.items), totalLabel(w.items))
	if w.sort.flat {
		tally += separated + flatLabel
	}

	return tally
}

func (w *worktreesScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
		tally:    w.tally(),
		help:     w.help,
		empty:    "no worktrees",
		rows:     len(w.rows(s)),
		snapshot: func(pane listPane) view.Snapshot { return w.snapshot(s, pane) },
	}
}

func (w *worktreesScreen) help(s panelShared) keyMap {
	return w.actions().help(s)
}

func (w *worktreesScreen) snapshot(s panelShared, pane listPane) view.Snapshot {
	rows := w.rows(s)

	return worktreesSnapshot(worktreesSnapshotParams{
		rows:          rows,
		items:         w.items,
		selected:      w.selected,
		focused:       rowKeyAt(rows, w.cursor),
		live:          s.listing.agents,
		flat:          w.sort.flat,
		caret:         pane.caret,
		barBlinkedOut: pane.blinkedOut,
		matcher:       s.chrome.filter.pattern,
		pane:          pane.width,
		now:           time.Now(),
	})
}

func (w *worktreesScreen) details(s panelShared) details {
	row, ok := rowAt(w.rows(s), w.cursor)
	if !ok {
		return details{}
	}

	if row.header {
		return details{
			id:     row.key(),
			fields: []recordField{{label: "repository", value: row.repo}, {label: "worktrees", value: fmt.Sprintf("%d", w.countUnder(row.repo))}},
		}
	}

	return details{id: row.key(), fields: worktreeFields(s, row.item, time.Now())}
}

func (w *worktreesScreen) countUnder(repo string) int {
	return len(itemsUnder(w.items, repo))
}

func worktreeFields(s panelShared, item worktreeItem, now time.Time) []recordField {
	fields := []recordField{
		{label: "name", value: worktreeRow{item: item}.name()},
		{label: "directory", value: item.dir},
		{label: "size", value: sizeField(item, now)},
		{label: "last active", value: lastActiveField(item, now)},
	}

	if !item.readable {
		fields = append(fields, recordField{label: "repository", value: label.OrDash(item.repoDir)})
		if len(item.occupants) > 0 {
			fields = append(fields, recordField{label: "also here", value: occupantsField(item)})
		}

		return append(fields, recordField{label: "agent", value: worktreeAgentField(s.listing, item)})
	}

	fields = append(fields,
		recordField{label: "branch", value: label.OrDash(item.branch)},
		recordField{label: "repository", value: label.OrDash(label.Path(item.repoDir, homeDir()))},
	)
	if len(item.occupants) > 0 {
		fields = append(fields, recordField{label: "also here", value: occupantsField(item)})
	}
	read := s.inspections[item.dir]
	if read.answered && read.err != nil {
		fields = append(fields, recordField{label: "worktree", value: "could not read: " + errui.GetReason(read.err)})
	}
	if read.isRead() {
		fields = append(fields,
			buildBaseField(read),
			recordField{label: "changes", value: changesField(read)},
		)
	}

	return append(fields, recordField{label: "agent", value: worktreeAgentField(s.listing, item)}, recordField{label: "workflow", value: worktreeWorkflowField(s.listing, item)})
}

func occupantsField(item worktreeItem) string {
	return strings.Join(xslice.Map(item.occupants, func(occupant worktreeAgent) string { return occupant.name }), ", ")
}

// sizeField is what the directory takes and when that was measured, or why it could not be.
func sizeField(item worktreeItem, now time.Time) string {
	switch {
	case item.size == nil:
		return label.Dash
	case item.size.err != "":
		return label.Dash + ", could not measure: " + item.size.err
	}

	return fmt.Sprintf("%s, measured %s ago", label.Bytes(item.size.bytes), label.AgeAt(item.size.measuredAt, now))
}

// lastActiveField is how long ago the directory was last touched.
func lastActiveField(item worktreeItem, now time.Time) string {
	if item.lastActiveAt.IsZero() {
		return label.Dash
	}

	field := label.AgeAt(item.lastActiveAt, now) + " ago"
	if item.agent != nil && item.agent.archivedAt != nil {
		field += ", archived"
	}

	return field
}

// buildBaseField spells where the branch stands against its base, or why git could not name one.
func buildBaseField(read worktreeInspection) recordField {
	if read.baseRef == "" {
		return recordField{label: "base", value: label.OrDash(read.baseError)}
	}
	if read.ahead == 0 && read.behind == 0 {
		return recordField{
			label:  "base",
			value:  read.baseRef + "  even",
			styled: read.baseRef + "  " + dimStyle.Render("even"),
		}
	}

	ahead := fmt.Sprintf("%s%d", glyph.Up, read.ahead)
	behind := fmt.Sprintf("%s%d", glyph.Down, read.behind)

	return recordField{
		label:  "base",
		value:  read.baseRef + "  " + ahead + " " + behind,
		styled: read.baseRef + "  " + countStyle(read.ahead, paint.Panel.Style(paint.ToneSuccess)).Render(ahead) + " " + countStyle(read.behind, paint.Panel.Style(paint.ToneMark)).Render(behind),
	}
}

func changesField(read worktreeInspection) string {
	var parts []string
	if n := len(read.modified); n > 0 {
		parts = append(parts, label.Count(n, "modified", "modified"))
	}
	if n := len(read.untracked); n > 0 {
		parts = append(parts, label.Count(n, "untracked", "untracked"))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

// worktreeAgentField is the agent behind a directory as the record names it.
func worktreeAgentField(l listing, item worktreeItem) string {
	if item.agent == nil {
		return "none"
	}
	if live, ok := l.liveAgent(item.agent.id); ok {
		return live.Agent.Harness + ", " + string(displayState(live))
	}
	if archived, ok := l.archivedAgent(item.agent.id); ok && archived.ArchivedAt != nil {
		return archived.Harness + ", archived " + label.Day(*archived.ArchivedAt)
	}
	if item.agent.archivedAt != nil {
		return "archived " + label.Day(*item.agent.archivedAt)
	}

	return string(item.agent.state)
}

func worktreeWorkflowField(l listing, item worktreeItem) string {
	if item.agent == nil {
		return label.Dash
	}

	workflowID := ""
	if live, ok := l.liveAgent(item.agent.id); ok {
		workflowID = live.Agent.WorkflowID
	} else if archived, ok := l.archivedAgent(item.agent.id); ok {
		workflowID = archived.WorkflowID
	}

	return l.workflowName(workflowID)
}

// doc is the markdown drawn beside the list for the row the cursor is on.
func (w *worktreesScreen) doc(s panelShared) string {
	doc := w.gitDoc(s)

	row, ok := w.hovered(s)
	if !ok || row.item.agent == nil {
		return doc
	}

	return doc + "\n" + conversationPreviewMarkdown(s.conversation, row.item.agent.id)
}

func (w *worktreesScreen) gitDoc(s panelShared) string {
	row, ok := rowAt(w.rows(s), w.cursor)
	switch {
	case !ok:
		return renderDocument("Worktrees", "_no worktrees_", "", "")
	case row.header:
		return renderDocument(label.Path(row.repo, homeDir()), fmt.Sprintf("_%s_", label.Count(w.countUnder(row.repo), "worktree", "worktrees")), "", "")
	}

	read, ok := s.inspections[row.item.dir]
	switch {
	case !ok || !read.answered:
		return renderDocument(row.name(), "_"+row.item.dir+"_", "_reading the directory…_", "")
	case read.err != nil && !read.isRead():
		return renderDocument(row.name(), "_"+row.item.dir+"_", "could not read: "+errui.GetReason(read.err), "")
	case read.err != nil:
		return renderDocument(row.name(), "_"+row.item.dir+"_", "_could not read: "+errui.GetReason(read.err)+"_\n\n"+worktreeProse(row.item, read), "")
	}

	return renderDocument(row.name(), "_"+row.item.dir+"_", worktreeProse(row.item, read), "")
}

// worktreeProse is the three blocks the document draws, each fenced.
func worktreeProse(item worktreeItem, read worktreeInspection) string {
	var b strings.Builder

	b.WriteString("## Contents\n\n")
	fenced(&b, entryLines(read.entries))

	if !item.readable {
		b.WriteString("\n_git cannot read this directory; its registration names a repository that is gone_\n")

		return b.String()
	}

	b.WriteString("\n## Uncommitted\n\n")
	if len(read.modified)+len(read.untracked) == 0 {
		b.WriteString("_nothing_\n")
	} else {
		fenced(&b, changeLines(read))
	}

	if read.baseRef != "" {
		fmt.Fprintf(&b, "\n## Commits since %s\n\n", read.baseRef)
		if len(read.commits) == 0 {
			b.WriteString("_none_\n")
		} else {
			fenced(&b, commitLines(read.commits))
		}
	}

	return b.String()
}

func fenced(b *strings.Builder, lines []string) {
	b.WriteString("```\n")
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	b.WriteString("```\n")
}

// entryLines is the biggest entries with their sizes lined up, and one line for the rest.
func entryLines(entries []worktreeEntry) []string {
	if len(entries) == 0 {
		return []string{"empty"}
	}

	width := 0
	for _, entry := range entries[:min(len(entries), entriesShown)] {
		width = max(width, len(entryName(entry)))
	}

	var lines []string
	var rest int64
	for i, entry := range entries {
		if i >= entriesShown {
			rest += entry.bytes

			continue
		}

		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, entryName(entry), label.Bytes(entry.bytes)))
	}
	if len(entries) > entriesShown {
		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, fmt.Sprintf("+ %d more", len(entries)-entriesShown), label.Bytes(rest)))
	}

	return lines
}

func entryName(entry worktreeEntry) string {
	if entry.dir {
		return entry.path + "/"
	}

	return entry.path
}

func changeLines(read worktreeInspection) []string {
	lines := xslice.Map(read.modified, func(path string) string {
		return "M " + path
	})
	lines = append(lines, xslice.Map(read.untracked, func(path string) string {
		return "? " + path
	})...)

	return lines
}

func commitLines(commits []worktreeCommit) []string {
	lines := xslice.Map(commits, func(commit worktreeCommit) string {
		return fmt.Sprintf("%s  %-4s %s", git.ShortSHA(commit.sha), label.Age(commit.committedAt), commit.subject)
	})

	return lines
}

var (
	_ detailed = (*worktreesScreen)(nil)
	_ filtered = (*worktreesScreen)(nil)
	_ tabbed   = (*worktreesScreen)(nil)
	_ columned = (*worktreesScreen)(nil)
	_ staged   = (*worktreesScreen)(nil)
)

func (w *worktreesScreen) doesDrawFromListing() bool {
	return true
}

func (w *worktreesScreen) takesEveryKey() bool {
	return false
}

func (w *worktreesScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Worktrees.Quit
}

func (w *worktreesScreen) detailsFit(s panelShared) bool {
	return s.draw.listFitsDetails(w.listParams(s))
}

func (w *worktreesScreen) detailsRoom(s panelShared) int {
	return s.draw.listRecordRoom(w.listParams(s))
}

func (w *worktreesScreen) hoveredKey(s panelShared) string {
	return w.list().hovered(s)
}

func (w *worktreesScreen) placeAt(h host, s panelShared, key string) {
	w.list().place(h, s, key)
}

func (w *worktreesScreen) filterRows(s panelShared) []filterRow {
	return w.list().filterRows(s)
}

func (w *worktreesScreen) tabs() tabStrip {
	return worktreesTabs()
}

func (w *worktreesScreen) tabsState() *tabsState {
	return &w.sort
}

func (w *worktreesScreen) tabsFit(s panelShared) bool {
	return s.draw.listFitsTabs(w.listParams(s))
}

func (w *worktreesScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.worktrees.doc(m.shared()) },
	}
}
