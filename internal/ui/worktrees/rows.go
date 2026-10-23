package worktrees

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

// row is one line of the worktrees list: a repository heading its directories, or a
// directory.
type row struct {
	header bool
	// repo is the repository the row sits under, which is the header's own name.
	repo string
	item panel.Worktree
}

// columns is the gutter of the worktrees list: a directory's size, its age, then where
// it stands against its base.
func columns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: sizeColumns},
		{Kind: view.ColumnText, Width: label.AgeWidth},
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}

// sizeColumns is how wide a directory's size is rendered.
const sizeColumns = 4

// noRepository heads the directories whose .git file names nothing.
const noRepository = "no repository"

func (r row) Key() string {
	if r.header {
		return repoRowKey(r.repo)
	}

	return treeRowKey(r.item.Dir)
}

func repoRowKey(repo string) string {
	return "repo:" + repo
}

func treeRowKey(dir string) string {
	return "tree:" + dir
}

// name is what the row is listed as.
func (r row) name() string {
	switch {
	case r.header:
		return label.Path(r.repo, homeDir())
	case r.item.Task != nil:
		return r.item.Task.Slug
	case r.item.Branch != "":
		return r.item.Branch
	}

	return filepath.Base(r.item.Dir)
}

// sorts is every column the worktrees list can be ordered by.
var sorts = []tabs.Column[panel.Worktree]{
	{
		ID:    "size",
		Label: "size",
		Has:   panel.Worktree.IsMeasured,
		Compare: func(a, b panel.Worktree) int {
			return cmp.Compare(a.Size.Bytes, b.Size.Bytes)
		},
	},
	{
		ID:    "last-accessed",
		Label: "last-accessed",
		Has: func(item panel.Worktree) bool {
			return !item.LastActiveAt.IsZero()
		},
		Compare: func(a, b panel.Worktree) int {
			return a.LastActiveAt.Compare(b.LastActiveAt)
		},
	},
}

type buildRowsParams struct {
	items []panel.Worktree
	order tabs.Order
	// flat orders the whole listing as one list.
	flat bool
}

// buildRows groups the listing under its repositories in path order, or orders it whole.
func buildRows(params buildRowsParams) []row {
	items, order := params.items, params.order
	if params.flat {
		members := slices.Clone(items)
		sortItems(members, order)

		return xslice.Map(members, func(item panel.Worktree) row {
			return row{item: item}
		})
	}

	byRepo := map[string][]panel.Worktree{}
	for _, item := range items {
		byRepo[item.RepoDir] = append(byRepo[item.RepoDir], item)
	}

	repos := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	slices.Sort(repos)

	var rows []row
	for _, repo := range repos {
		members := byRepo[repo]
		heading := repoHeading(members[0])
		rows = append(rows, row{header: true, repo: heading})

		sortItems(members, order)
		rows = append(rows, xslice.Map(members, func(item panel.Worktree) row {
			return row{repo: heading, item: item}
		})...)
	}

	return rows
}

// sortItems orders the directories by the column, the path settling a tie.
func sortItems(items []panel.Worktree, order tabs.Order) {
	tabs.Sort(tabs.SortParams[panel.Worktree]{
		Rows:    items,
		Columns: sorts,
		Order:   order,
		Tiebreak: func(a, b panel.Worktree) int {
			return strings.Compare(a.Dir, b.Dir)
		},
	})
}

type rowsKey struct {
	revision int
	scope    scope
	order    tabs.Order
	flat     bool
	filter   list.PatternKey
}

func (w *Screen) rows(in screen.Inputs) []row {
	order := sortStrip().Order(w.sort)
	key := rowsKey{
		revision: w.revision,
		scope:    w.scope,
		order:    order,
		flat:     w.sort.Flat,
		filter:   in.Room.Pattern.Key(),
	}

	return w.built.Read(key, func() []row {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[row]{
			Rows: buildRows(buildRowsParams{
				items: w.shown(),
				order: order,
				flat:  w.sort.Flat,
			}),
			Depth: rowDepth,
			Name:  row.name,
		})
	})
}

func rowDepth(r row) int {
	if r.header {
		return 0
	}

	return 1
}

// itemsUnder is the directories a repository heads, read off the whole listing.
func itemsUnder(items []panel.Worktree, repo string) []panel.Worktree {
	return xslice.Filter(items, func(item panel.Worktree) bool {
		return repoHeading(item) == repo
	})
}

func repoHeading(item panel.Worktree) string {
	if item.RepoDir == "" {
		return noRepository
	}

	return item.RepoDir
}

// refs is the worktrees list as the selection sees it.
func refs(rows []row) []list.Ref {
	return xslice.Map(rows, func(r row) list.Ref {
		return list.Ref{
			Key:    r.Key(),
			Header: r.header,
			Group:  repoRowKey(r.repo),
		}
	})
}

// rowIndexOfAgent is the row of the directory an agent was cut for or works in.
func rowIndexOfAgent(rows []row, id string) int {
	if id == "" {
		return -1
	}

	for i, r := range rows {
		if r.header {
			continue
		}
		if r.item.HoldsAgent(id) {
			return i
		}
	}

	return -1
}

// listSnapshotParams is what the panel holds about the worktrees list, as plain data.
type listSnapshotParams struct {
	rows []row
	// items is the whole listing, which is what a repository's own figures are read off.
	items    []panel.Worktree
	selected list.Selection
	focused  string
	// live are the agents the agents listing holds, whose marks a row with one of them renders.
	live []panel.AgentRow
	// flat renders every directory at the heading indent.
	flat    bool
	caret   view.Caret
	matcher *regexp.Regexp
	pane    int
	now     time.Time
}

// listSnapshot reads the rows into the plain data the list renders itself from.
func listSnapshot(params listSnapshotParams) view.Snapshot {
	live := xslice.KeyBy(params.live, func(item panel.AgentRow) string { return item.Agent.ID })

	rows := make([]view.Row, 0, len(params.rows))
	for _, r := range params.rows {
		row := view.Row{
			Key:     r.Key(),
			Name:    r.name(),
			Matches: list.Matches(params.matcher, r.name()),
		}

		switch {
		case r.header:
			row.Kind = view.KindWorkflow
			row.Process = view.ProcessNone
			row.Cells = groupCells(params.items, r.repo)
		default:
			row.Kind = view.KindAgent
			if params.flat {
				row.Kind = view.KindResult
			}
			row.Cells = rowCells(r.item, params.now)
			if occupant, ok := r.item.FirstOccupant(); ok {
				row = agentFacts(row, occupant, live)
			} else {
				row.Process = view.ProcessStopped
			}
		}
		row.Selected = params.selected.Has(row.Key)

		rows = append(rows, row)
	}

	return view.Snapshot{
		Rows:    rows,
		Caret:   params.caret,
		Focused: params.focused,
		Pane:    params.pane,
		Columns: columns(),
	}
}

// agentFacts is what a row says about the agent behind it.
func agentFacts(row view.Row, agent panel.WorktreeAgent, live map[string]panel.AgentRow) view.Row {
	item, ok := live[agent.ID]
	if !ok {
		item = panel.AgentRow{Agent: panel.Agent{
			ID:         agent.ID,
			Name:       agent.Name,
			State:      agent.State,
			ArchivedAt: agent.ArchivedAt,
		}}
	}

	return item.Facts(row)
}

// rowCells are a directory's size, its age, then where it stands.
func rowCells(item panel.Worktree, now time.Time) []view.Cell {
	return []view.Cell{
		{Text: sizeLabel(item), Tone: paint.ToneMuted},
		{Text: label.AgeAt(item.LastActiveAt, now), Tone: paint.ToneMuted},
		standingCell(item),
	}
}

// groupCells are the total of the measured directories under a header, over the size column.
func groupCells(items []panel.Worktree, repo string) []view.Cell {
	return []view.Cell{{Text: totalLabel(itemsUnder(items, repo)), Tone: paint.ToneMuted}}
}

// sizeLabel is what a directory takes, or a dash while it is unmeasured.
func sizeLabel(item panel.Worktree) string {
	if !item.IsMeasured() {
		return label.Dash
	}

	return label.Bytes(item.Size.Bytes)
}

// standingCell is where a directory stands against its base, or a dash while git has not said.
func standingCell(item panel.Worktree) view.Cell {
	standing := item.Standing
	switch {
	case !item.Readable:
		return view.Cell{Glyph: glyph.WorktreeGone, Tone: paint.ToneDanger}
	case standing == nil || standing.Err != "":
		return view.Cell{Text: label.Dash, Tone: paint.ToneMuted}
	case standing.Dirty || standing.Sequencing:
		return view.Cell{Glyph: glyph.Worktree, Tone: paint.ToneWarning}
	case standing.Unlanded > 0:
		return view.Cell{Glyph: glyph.Worktree, Tone: paint.ToneMuted}
	}

	return view.Cell{Glyph: glyph.Merged, Tone: paint.ToneMerged}
}

// totalLabel is what the measured directories take together.
func totalLabel(items []panel.Worktree) string {
	var total int64
	measured := false
	for _, item := range items {
		if !item.IsMeasured() {
			continue
		}

		total += item.Size.Share
		measured = true
	}

	if !measured {
		return label.Dash
	}

	return label.Bytes(total)
}
