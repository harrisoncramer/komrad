package ui

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/internal/view"
	"weave/lib/xslice"
)

// worktreeRow is one line of the worktrees list: a repository heading its directories, or a
// directory.
type worktreeRow struct {
	header bool
	// repo is the repository the row sits under, which is the header's own name.
	repo string
	item worktreeItem
}

// worktreesDropped are the two columns the worktrees list gives up.
var worktreesDropped = view.Columns{
	Review: true,
	Forge:  true,
}

// noRepository heads the directories whose .git file names nothing.
const noRepository = "no repository"

func (r worktreeRow) key() string {
	if r.header {
		return repoRowKey(r.repo)
	}

	return treeRowKey(r.item.dir)
}

func repoRowKey(repo string) string {
	return "repo:" + repo
}

func treeRowKey(dir string) string {
	return "tree:" + dir
}

// name is what the row is listed as.
func (r worktreeRow) name() string {
	switch {
	case r.header:
		return label.Path(r.repo, homeDir())
	case r.item.agent != nil:
		return r.item.agent.name
	case r.item.branch != "":
		return r.item.branch
	}

	return filepath.Base(r.item.dir)
}

// worktreeSorts is every column the worktrees list can be ordered by.
var worktreeSorts = []sortColumn[worktreeItem]{
	{
		id:    "size",
		label: "size",
		has:   worktreeItem.isMeasured,
		compare: func(a, b worktreeItem) int {
			return cmp.Compare(a.size.bytes, b.size.bytes)
		},
	},
	{
		id:    "last-accessed",
		label: "last-accessed",
		has: func(item worktreeItem) bool {
			return !item.lastActiveAt.IsZero()
		},
		compare: func(a, b worktreeItem) int {
			return a.lastActiveAt.Compare(b.lastActiveAt)
		},
	},
}

// worktreeRows groups the listing under its repositories in path order.
func worktreeRows(items []worktreeItem, order sortOrder) []worktreeRow {
	byRepo := map[string][]worktreeItem{}
	for _, item := range items {
		byRepo[item.repoDir] = append(byRepo[item.repoDir], item)
	}

	repos := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	slices.Sort(repos)

	var rows []worktreeRow
	for _, repo := range repos {
		members := byRepo[repo]
		heading := repoHeading(members[0])
		rows = append(rows, worktreeRow{header: true, repo: heading})

		sortRows(sortRowsParams[worktreeItem]{
			rows:    members,
			columns: worktreeSorts,
			order:   order,
			tiebreak: func(a, b worktreeItem) int {
				return strings.Compare(a.dir, b.dir)
			},
		})
		rows = append(rows, xslice.Map(members, func(item worktreeItem) worktreeRow {
			return worktreeRow{repo: heading, item: item}
		})...)
	}

	return rows
}

func (m model) worktreeRows() []worktreeRow {
	if m.worktrees.rows != nil {
		return m.worktrees.rows
	}

	return m.buildWorktreeRows()
}

func (m model) buildWorktreeRows() []worktreeRow {
	return narrowed(m.filter, narrowedParams[worktreeRow]{
		rows:  worktreeRows(m.worktrees.items, worktreesTabs().order(m.worktrees.sort)),
		depth: worktreeRowDepth,
		name:  worktreeRow.name,
	})
}

func worktreeRowDepth(row worktreeRow) int {
	if row.header {
		return 0
	}

	return 1
}

// itemsUnder is the directories a repository heads, read off the whole listing.
func itemsUnder(items []worktreeItem, repo string) []worktreeItem {
	return xslice.Filter(items, func(item worktreeItem) bool {
		return repoHeading(item) == repo
	})
}

func repoHeading(item worktreeItem) string {
	if item.repoDir == "" {
		return noRepository
	}

	return item.repoDir
}

// worktreeRefs is the worktrees list as the selection sees it.
func worktreeRefs(rows []worktreeRow) []rowRef {
	return xslice.Map(rows, func(row worktreeRow) rowRef {
		return rowRef{
			key:    row.key(),
			header: row.header,
			group:  repoRowKey(row.repo),
		}
	})
}

// rowIndexOfWorktreeAgent is the row of the directory an agent was cut for or works in.
func rowIndexOfWorktreeAgent(rows []worktreeRow, id string) int {
	if id == "" {
		return -1
	}

	for i, r := range rows {
		if r.header {
			continue
		}
		if agent := r.item.agent; agent != nil && agent.id == id {
			return i
		}
		if slices.ContainsFunc(r.item.occupants, func(occupant worktreeAgent) bool { return occupant.id == id }) {
			return i
		}
	}

	return -1
}

// worktreesSnapshotParams is what the panel holds about the worktrees list, as plain data.
type worktreesSnapshotParams struct {
	rows []worktreeRow
	// items is the whole listing, which is what a repository's own figures are read off.
	items    []worktreeItem
	selected selection
	focused  string
	// live are the agents the agents listing holds, whose marks a row with one of them draws.
	live          []agentItem
	caretHidden   bool
	barBlinkedOut bool
	matcher       *regexp.Regexp
	pane          int
	now           time.Time
}

// worktreesSnapshot reads the rows into the plain data the list draws itself from.
func worktreesSnapshot(params worktreesSnapshotParams) view.Snapshot {
	live := xslice.KeyBy(params.live, func(item agentItem) string { return item.agent.ID })

	rows := make([]view.Row, 0, len(params.rows))
	for _, r := range params.rows {
		row := view.Row{
			Key:     r.key(),
			Name:    r.name(),
			Matches: matchesIn(params.matcher, r.name()),
		}

		switch {
		case r.header:
			row.Kind = view.KindWorkflow
			row.Process = view.ProcessNone
			row.Trail = groupTrail(params.items, r.repo)
		default:
			row.Kind = view.KindAgent
			row.Repo = repoStanding(r.item)
			row.Trail = rowTrail(r.item, params.now)
			if r.item.agent != nil {
				row = worktreeAgentFacts(row, *r.item.agent, live)
			} else {
				row.Process = view.ProcessStopped
			}
		}
		row.Selected = params.selected.has(row.Key)

		rows = append(rows, row)
	}

	return view.Snapshot{
		Rows:          rows,
		CaretHidden:   params.caretHidden,
		BarBlinkedOut: params.barBlinkedOut,
		Focused:       params.focused,
		Pane:          params.pane,
		Dropped:       worktreesDropped,
	}
}

// worktreeAgentFacts is what a row says about the agent behind it.
func worktreeAgentFacts(row view.Row, agent worktreeAgent, live map[string]agentItem) view.Row {
	item, ok := live[agent.id]
	if !ok {
		item = agentItem{agent: panel.Agent{
			ID:         agent.id,
			Name:       agent.name,
			State:      agent.state,
			ArchivedAt: agent.archivedAt,
		}}
	}

	return agentFacts(row, item)
}

func repoStanding(item worktreeItem) view.Repo {
	if !item.readable {
		return view.RepoGone
	}

	return view.RepoClean
}

// rowTrail is a directory's size then its age.
func rowTrail(item worktreeItem, now time.Time) string {
	return view.Trail(sizeLabel(item), label.AgeAt(item.lastActiveAt, now))
}

// groupTrail is the total of the measured directories under a header, over the size column.
func groupTrail(items []worktreeItem, repo string) string {
	return view.Trail(totalLabel(itemsUnder(items, repo)), "")
}

// sizeLabel is what a directory takes, or a dash while it is unmeasured.
func sizeLabel(item worktreeItem) string {
	if !item.isMeasured() {
		return label.Dash
	}

	return label.Bytes(item.size.bytes)
}

// totalLabel is what the measured directories take together.
func totalLabel(items []worktreeItem) string {
	var total int64
	measured := false
	for _, item := range items {
		if !item.isMeasured() {
			continue
		}

		total += item.size.bytes
		measured = true
	}

	if !measured {
		return label.Dash
	}

	return label.Bytes(total)
}
