package ui

import (
	"fmt"
	"slices"
	"strings"

	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/view"
	"weave/lib/dates"

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
	m.worktrees.focus = agentID
	if idx := rowIndexOfWorktreeAgent(m.worktreeRows(), agentID); idx >= 0 {
		m.worktrees.cursor = idx
	}

	return m.enterWorktrees()
}

func (m model) hoveredWorktreeKey() string {
	return rowKeyAt(m.worktreeRows(), m.worktrees.cursor)
}

func (m *model) placeWorktreeCursor(hovered string) {
	rows := m.worktreeRows()
	m.landWorktreeCursor(rows, placeCursor(rowIndexOfKey(rows, hovered), m.worktrees.cursor, len(rows)))
	m.stageWorktrees()
}

func (m model) worktreeFilterRows() []filterRow {
	return worktreeFilterRows(m.worktreeRows())
}

func (m model) hoveredWorktree() (worktreeRow, bool) {
	row, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)
	if !ok || row.header {
		return worktreeRow{}, false
	}

	return row, true
}

func (m model) updateWorktrees(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return m.jumpWorktreeCursor(0)
	}
	if m.topHeld {
		return m, nil
	}

	return worktreesActions().dispatch(m, k)
}

func (m model) moveWorktreeCursor(delta int) (tea.Model, tea.Cmd) {
	rows := m.worktreeRows()

	next, ok := m.worktrees.move(delta, len(rows))
	if !ok {
		return m, nil
	}

	m.landWorktreeCursor(rows, next.cursor)

	return m.deferStaging()
}

func (m model) jumpWorktreeCursor(idx int) (tea.Model, tea.Cmd) {
	rows := m.worktreeRows()

	next, ok := m.worktrees.jump(idx, len(rows))
	if !ok {
		return m, nil
	}

	m.landWorktreeCursor(rows, next.cursor)

	return m.deferStaging()
}

// stageWorktrees shows the hovered agent's pane where it has one and the document otherwise.
func (m *model) stageWorktrees() {
	m.frame.Begin()
	defer m.afterStaging()

	rows := m.worktreeRows()
	m.placeColumn(screenWorktrees, rowKeyAt(rows, m.worktrees.cursor))
	m.reconcileSolo()

	item, held := m.hoveredWorktreePane()
	switch {
	case held && m.isPaneStageable(item.agent.Pane, item.agent.Harness):
		m.sizeGuestWindow(item.agent.Pane)
		m.frame.ShowGuest(item.agent.Pane)
	case m.frame.IsSolo() && m.isGuestLive():
	default:
		m.frame.Show(layout.RegionStage, newBlankContent())
	}

	m.stageInfo()
}

// hoveredWorktreePane is the live agent whose pane the hovered row shows, if the row has one.
func (m model) hoveredWorktreePane() (agentItem, bool) {
	row, ok := m.hoveredWorktree()
	if !ok {
		return agentItem{}, false
	}

	agent := row.agentOn()
	if agent == nil {
		return agentItem{}, false
	}

	item, ok := m.liveAgent(agent.id)
	if !ok || !hasPane(item) {
		return agentItem{}, false
	}

	return item, true
}

// hasNoWorktreePane reports whether the hovered row has no pane of its own to stand in the
// window.
func (m model) hasNoWorktreePane() bool {
	_, held := m.hoveredWorktreePane()

	return !held
}

// inspectWorktreeCmd asks the service for the hovered directory's standing, answered under the
// directory's own name, and asks nothing while an answer for it is still on its way.
func (m *model) inspectWorktreeCmd() tea.Cmd {
	row, ok := m.hoveredWorktree()
	if !ok || row.occupant != nil {
		return nil
	}
	if read, asked := m.worktrees.inspected[row.item.dir]; asked && read.pending {
		return nil
	}

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: row.item.dir, inspection: worktreeInspection{pending: true}})
	clients, dir := m.api, row.item.dir

	return func() tea.Msg {
		read, err := clients.getWorktree(dir)
		if err != nil {
			return worktreeInspectedMsg{dir: dir, inspection: worktreeInspection{err: err}}
		}

		return worktreeInspectedMsg{dir: dir, inspection: worktreeInspectionFrom(read)}
	}
}

// inspectHoveredWorktreeCmd asks for the hovered directory's standing where nothing has been
// asked for it yet, which is how a row the screen opened on is read without a move.
func (m *model) inspectHoveredWorktreeCmd() tea.Cmd {
	if m.screen != screenWorktrees {
		return nil
	}

	row, ok := m.hoveredWorktree()
	if !ok || row.occupant != nil {
		return nil
	}
	if _, asked := m.worktrees.inspected[row.item.dir]; asked {
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
	if m.worktrees.inspected == nil {
		m.worktrees.inspected = map[string]worktreeInspection{}
	}

	m.worktrees.inspected[msg.dir] = msg.inspection
}

func worktreesColumn() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   model.worktreeDoc,
		holds: model.hasNoWorktreePane,
	}
}

func (m model) worktreesView() string {
	return m.columnView(model.worktreesListView)
}

func (m model) worktreesHeader() string {
	return m.listHeader("Worktrees" + dimStyle.Render("  "+m.worktreesTally()))
}

// worktreesTally is how many directories the listing holds and what the counted ones hold.
func (m model) worktreesTally() string {
	var total int64
	counted := false
	for _, item := range m.worktrees.items {
		if item.bytes != nil {
			total += *item.bytes
			counted = true
		}
	}

	size := "—"
	if counted {
		size = bytesLabel(total)
	}

	return fmt.Sprintf("%d · %s", len(m.worktrees.items), size)
}

func (m model) doesWorktreesFitDetails() bool {
	return m.doesListFitDetails(m.worktreesHeader(), model.worktreesHelp)
}

func (m model) worktreesListView() string {
	header := m.worktreesHeader()
	record, help := m.detailsView(), m.renderHelp(m.worktreesHelp(), header)
	below := record + m.messagesRegion(record, help, header) + help

	rows := m.worktreeRows()
	if len(rows) == 0 {
		return m.headedListView(header, []string{dimStyle.Render("no worktrees")}, 0, below)
	}

	pane := m.paneWidth()
	list := view.Prepare(m.worktreesSnapshot(rows, pane))

	lines := m.listLines(header, below)
	window := view.Window(view.WindowParams{
		List:    list,
		Palette: paint.Panel,
		Width:   pane,
		Lines:   lines,
	})

	return m.headedBlockView(header, window, lines, below)
}

func (m model) worktreesSnapshot(rows []worktreeRow, pane int) view.Snapshot {
	return worktreesSnapshot(worktreesSnapshotParams{
		rows:          rows,
		selected:      m.worktrees.selected,
		focused:       rowKeyAt(rows, m.worktrees.cursor),
		live:          m.listing.agents,
		caretHidden:   m.cursorBlinkedOut(m.isListFocused()),
		barBlinkedOut: m.rowCursor.Blink,
		matcher:       m.filter.pattern,
		pane:          pane,
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

	if row.occupant != nil {
		return m.occupantDetails(row)
	}

	return details{id: row.key(), fields: m.worktreeFields(row.item)}
}

func (m model) countUnder(repo string) int {
	count := 0
	for _, row := range m.worktreeRows() {
		if !row.header && row.occupant == nil && row.repo == repo {
			count++
		}
	}

	return count
}

// occupantDetails is the agents list's own record for an agent working in somebody else's
// directory, where the listing holds it, and the little the worktree listing says otherwise.
func (m model) occupantDetails(row worktreeRow) details {
	if item, ok := m.liveAgent(row.occupant.id); ok {
		return details{id: row.key(), fields: agentFields(item.agent, m.scheduleName(item.agent), m.parentName(item.agent))}
	}
	if agent, ok := m.archivedAgent(row.occupant.id); ok {
		return details{id: row.key(), fields: archiveFields(agent, m.scheduleName(agent))}
	}

	return details{id: row.key(), fields: []recordField{
		{"name", row.occupant.name},
		{"directory", row.item.dir},
		{"agent_id", row.occupant.id},
	}}
}

func (m model) worktreeFields(item worktreeItem) []recordField {
	fields := []recordField{
		{"name", worktreeRow{item: item}.name()},
		{"directory", item.dir},
		{"size", sizeTrail(item)},
	}

	if !item.readable {
		return append(fields,
			recordField{"repository", orDash(item.repoDir)},
			recordField{"agent", m.worktreeAgentField(item)},
		)
	}

	fields = append(fields, recordField{"branch", orDash(item.branch)})
	if read, ok := m.worktrees.inspected[item.dir]; ok && !read.pending && read.err == nil {
		fields = append(fields,
			recordField{"base", baseField(read)},
			recordField{"changes", changesField(read)},
		)
	}

	return append(fields, recordField{"agent", m.worktreeAgentField(item)}, recordField{"workflow", m.worktreeWorkflowField(item)})
}

// baseField spells where the branch stands against its base, or a dash when git names none.
func baseField(read worktreeInspection) string {
	if read.baseRef == "" {
		return orDash("")
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
		parts = append(parts, countWord(n, "modified", "modified"))
	}
	if n := len(read.untracked); n > 0 {
		parts = append(parts, countWord(n, "untracked", "untracked"))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

// worktreeAgentField is the agent behind a directory as the record names it, read off the
// listing that holds the agent.
func (m model) worktreeAgentField(item worktreeItem) string {
	if item.agent == nil {
		return "none"
	}
	if live, ok := m.liveAgent(item.agent.id); ok {
		return live.agent.Harness + ", " + string(displayState(live))
	}
	if archived, ok := m.archivedAgent(item.agent.id); ok && archived.ArchivedAt != nil {
		return archived.Harness + ", archived " + dates.Day(*archived.ArchivedAt)
	}
	if item.agent.archivedAt != nil {
		return "archived " + dates.Day(*item.agent.archivedAt)
	}

	return string(item.agent.state)
}

func (m model) worktreeWorkflowField(item worktreeItem) string {
	if item.agent == nil {
		return orDash("")
	}

	workflowID := ""
	if live, ok := m.liveAgent(item.agent.id); ok {
		workflowID = live.agent.WorkflowID
	} else if archived, ok := m.archivedAgent(item.agent.id); ok {
		workflowID = archived.WorkflowID
	}

	for _, w := range slices.Concat(m.listing.workflows, m.archive.workflows) {
		if w.ID == workflowID {
			return w.Name
		}
	}

	return orDash("")
}

// worktreeDoc is the markdown drawn beside the list: what the hovered directory holds and how
// its branch stands.
func (m model) worktreeDoc() string {
	row, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)
	switch {
	case !ok:
		return renderDocument("Worktrees", "_no worktrees_", "", "")
	case row.header:
		return renderDocument(shortenHome(row.repo), fmt.Sprintf("_%s_", countWord(m.countUnder(row.repo), "worktree", "worktrees")), "", "")
	case row.occupant != nil:
		return renderDocument(row.occupant.name, fmt.Sprintf("_working in %s_", row.item.dir), "", "")
	}

	read, ok := m.worktrees.inspected[row.item.dir]
	switch {
	case !ok || read.pending:
		return renderDocument(row.name(), "_"+row.item.dir+"_", "_reading the directory…_", "")
	case read.err != nil:
		return renderDocument(row.name(), "_"+row.item.dir+"_", read.err.Error(), "")
	}

	return renderDocument(row.name(), "_"+row.item.dir+"_", worktreeProse(row.item, read), "")
}

// worktreeProse is the three blocks the document draws, each fenced.
func worktreeProse(item worktreeItem, read worktreeInspection) string {
	var b strings.Builder

	b.WriteString("## Contents\n\n")
	if item.bytes == nil {
		b.WriteString("_not counted yet_\n")
	} else {
		fenced(&b, entryLines(read.entries))
	}

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

		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, entryName(entry), bytesLabel(entry.bytes)))
	}
	if len(entries) > entriesShown {
		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, fmt.Sprintf("+ %d more", len(entries)-entriesShown), bytesLabel(rest)))
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
	var lines []string
	for _, path := range read.modified {
		lines = append(lines, "M "+path)
	}
	for _, path := range read.untracked {
		lines = append(lines, "? "+path)
	}

	return lines
}

func commitLines(commits []worktreeCommit) []string {
	var lines []string
	for _, commit := range commits {
		lines = append(lines, fmt.Sprintf("%s  %-4s %s", shortSHA(commit.sha), dates.Age(commit.committedAt), commit.subject))
	}

	return lines
}
