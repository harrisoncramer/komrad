package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/config"
	"weave/internal/errui"
	"weave/internal/git"
	"weave/internal/ui/paint/label"
	"weave/internal/view"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
)

// entriesShown is how many of a directory's entries the document lists before counting the rest.
const entriesShown = 10

func (m model) enterWorktrees() (tea.Model, tea.Cmd) {
	m.enterScreen(screenWorktrees)
	m.stageWorktrees()

	return m, m.refreshCmd()
}

// openWorktrees is the key's own way onto the screen, landing on the hovered agent's directory.
func (m model) openWorktrees(agentID string) (tea.Model, tea.Cmd) {
	m.enterScreen(screenWorktrees)

	m.worktrees.focus = agentID
	if idx := rowIndexOfWorktreeAgent(m.worktreeRows(), agentID); idx >= 0 {
		m.worktrees.cursor = idx
	}

	return m.enterWorktrees()
}

func worktreesList() listSpec[worktreeRow] {
	return listSpec[worktreeRow]{
		cursor: func(m *model) *rowCursor { return &m.worktrees.rowCursor },
		rows:   model.worktreeRows,
		name:   worktreeRow.name,
		refs:   worktreeRefs,
		header: "repository",
		placed: (*model).stageWorktrees,
		moved: func(m model) (model, tea.Cmd) {
			m.placeColumn(screenWorktrees, worktreesList().hovered(m))

			return m.deferStaging()
		},
		quit: func(k config.Keybindings) []string { return k.Worktrees.Quit },
		back: func(k config.Keybindings) []string { return k.Worktrees.Back },
	}
}

func worktreesChrome() listChrome {
	return listChrome{
		title: screenTitle(view.GlyphRepo, "Worktrees"),
		tally: model.worktreesTally,
		help:  model.worktreesHelp,
	}
}

func (m model) hoveredWorktree() (worktreeRow, bool) {
	row, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)
	if !ok || row.header {
		return worktreeRow{}, false
	}

	return row, true
}

func (m model) updateWorktrees(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return worktreesList().update(m, key, worktreesActions())
}

func (m *model) stageWorktrees() {
	m.placeColumn(screenWorktrees, worktreesList().hovered(*m))
	m.stageInfo()
}

// inspectWorktreeCmd asks the service for the hovered directory's standing.
func (m *model) inspectWorktreeCmd() tea.Cmd {
	row, ok := m.hoveredWorktree()
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

	row, ok := m.hoveredWorktree()
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

func worktreesColumn() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   model.worktreeDoc,
	}
}

func (m model) worktreesView() string {
	m.worktrees.rows = m.buildWorktreeRows()

	return m.columnView(model.worktreesListView)
}

func worktreesTabs() tabStrip {
	return tabStrip{
		title:    "↑↓",
		legend:   "sort",
		options:  tabOptionsOf(worktreeSorts),
		fallback: "size",
		grouped:  "repo",
	}
}

// worktreesTally is how many directories the listing holds, what the measured ones take, and
// whether they are drawn as one list.
func (m model) worktreesTally() string {
	tally := fmt.Sprintf("%d · %s", len(m.worktrees.items), totalLabel(m.worktrees.items))
	if m.worktrees.sort.flat {
		tally += " · " + flatLabel
	}

	return tally
}

func (m model) worktreesListView() string {
	return m.listView(listViewParams{
		chrome:   worktreesChrome(),
		empty:    "no worktrees",
		rows:     len(m.worktreeRows()),
		snapshot: model.worktreesSnapshot,
	})
}

func (m model) worktreesSnapshot(pane int) view.Snapshot {
	rows := m.worktreeRows()

	return worktreesSnapshot(worktreesSnapshotParams{
		rows:          rows,
		items:         m.worktrees.items,
		selected:      m.worktrees.selected,
		focused:       rowKeyAt(rows, m.worktrees.cursor),
		live:          m.listing.agents,
		flat:          m.worktrees.sort.flat,
		caretHidden:   m.isCursorBlinkedOut(m.isListFocused()),
		barBlinkedOut: m.rowCursor.Blink,
		matcher:       m.filter.pattern,
		pane:          pane,
		now:           time.Now(),
	})
}

func (m model) worktreesDetails() details {
	row, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)
	if !ok {
		return details{}
	}

	if row.header {
		return details{
			id:     row.key(),
			fields: []recordField{{"repository", row.repo}, {"worktrees", fmt.Sprintf("%d", m.countUnder(row.repo))}},
		}
	}

	return details{id: row.key(), fields: m.worktreeFields(row.item, time.Now())}
}

func (m model) countUnder(repo string) int {
	return len(itemsUnder(m.worktrees.items, repo))
}

func (m model) worktreeFields(item worktreeItem, now time.Time) []recordField {
	fields := []recordField{
		{"name", worktreeRow{item: item}.name()},
		{"directory", item.dir},
		{"size", sizeField(item, now)},
		{"last active", lastActiveField(item, now)},
	}

	if !item.readable {
		fields = append(fields, recordField{"repository", label.OrDash(item.repoDir)})
		if len(item.occupants) > 0 {
			fields = append(fields, recordField{"also here", occupantsField(item)})
		}

		return append(fields, recordField{"agent", m.worktreeAgentField(item)})
	}

	fields = append(fields,
		recordField{"branch", label.OrDash(item.branch)},
		recordField{"repository", label.OrDash(label.Path(item.repoDir, homeDir()))},
	)
	if len(item.occupants) > 0 {
		fields = append(fields, recordField{"also here", occupantsField(item)})
	}
	read := m.inspections[item.dir]
	if read.answered && read.err != nil {
		fields = append(fields, recordField{"worktree", "could not read: " + errui.GetReason(read.err)})
	}
	if read.isRead() {
		fields = append(fields,
			recordField{"base", baseField(read)},
			recordField{"changes", changesField(read)},
		)
	}

	return append(fields, recordField{"agent", m.worktreeAgentField(item)}, recordField{"workflow", m.worktreeWorkflowField(item)})
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

// baseField spells where the branch stands against its base, or why git could not name one.
func baseField(read worktreeInspection) string {
	if read.baseRef == "" {
		return label.OrDash(read.baseError)
	}

	var parts []string
	if read.ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", read.ahead))
	}
	if read.behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", read.behind))
	}
	if len(parts) == 0 {
		return read.baseRef
	}

	return read.baseRef + "  " + strings.Join(parts, " ")
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
func (m model) worktreeAgentField(item worktreeItem) string {
	if item.agent == nil {
		return "none"
	}
	if live, ok := m.liveAgent(item.agent.id); ok {
		return live.agent.Harness + ", " + string(displayState(live))
	}
	if archived, ok := m.archivedAgent(item.agent.id); ok && archived.ArchivedAt != nil {
		return archived.Harness + ", archived " + label.Day(*archived.ArchivedAt)
	}
	if item.agent.archivedAt != nil {
		return "archived " + label.Day(*item.agent.archivedAt)
	}

	return string(item.agent.state)
}

func (m model) worktreeWorkflowField(item worktreeItem) string {
	if item.agent == nil {
		return label.Dash
	}

	workflowID := ""
	if live, ok := m.liveAgent(item.agent.id); ok {
		workflowID = live.agent.WorkflowID
	} else if archived, ok := m.archivedAgent(item.agent.id); ok {
		workflowID = archived.WorkflowID
	}

	return m.workflowName(workflowID)
}

// workflowName is what a workflow is called, live or archived, or a dash when none has the id.
func (m model) workflowName(id string) string {
	for _, w := range slices.Concat(m.listing.workflows, m.listing.archivedWorkflows) {
		if w.ID == id {
			return w.Name
		}
	}

	return label.Dash
}

// worktreeDoc is the markdown drawn beside the list for the row the cursor is on.
func (m model) worktreeDoc() string {
	row, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)
	switch {
	case !ok:
		return renderDocument("Worktrees", "_no worktrees_", "", "")
	case row.header:
		return renderDocument(label.Path(row.repo, homeDir()), fmt.Sprintf("_%s_", label.Count(m.countUnder(row.repo), "worktree", "worktrees")), "", "")
	}

	read, ok := m.inspections[row.item.dir]
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
