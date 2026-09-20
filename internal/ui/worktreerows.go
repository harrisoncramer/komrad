package ui

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/ui/paint/label"
	"weave/internal/view"
	"weave/lib/dates"
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

// worktreesDropped are the two columns the worktrees list gives up: the verdict rides the live
// listing's tally and nothing fills the forge.
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

// name is what the row is listed as: the agent's name when one answers, the branch when none
// does, and the directory's own name when it has neither.
func (r worktreeRow) name() string {
	switch {
	case r.header:
		return shortenHome(r.repo)
	case r.item.agent != nil:
		return r.item.agent.name
	case r.item.branch != "":
		return r.item.branch
	}

	return filepath.Base(r.item.dir)
}

// worktreeRows groups the listing under its repositories, each group holding its live rows
// first, then the archived, then the directories nothing answers for.
func worktreeRows(items []worktreeItem) []worktreeRow {
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
		heading := repo
		if heading == "" {
			heading = noRepository
		}
		rows = append(rows, worktreeRow{header: true, repo: heading})

		members := byRepo[repo]
		slices.SortStableFunc(members, func(a, b worktreeItem) int {
			if by := standingRank(a) - standingRank(b); by != 0 {
				return by
			}

			return strings.Compare(worktreeRow{item: a}.name(), worktreeRow{item: b}.name())
		})
		for _, item := range members {
			rows = append(rows, worktreeRow{repo: heading, item: item})
		}
	}

	return rows
}

// standingRank orders a group: a directory with a live agent, then one whose agent is put
// away, then one nothing answers for.
func standingRank(item worktreeItem) int {
	switch {
	case item.agent == nil:
		return 2
	case item.agent.archivedAt != nil:
		return 1
	}

	return 0
}

func (m model) worktreeRows() []worktreeRow {
	return worktreeRows(m.worktrees.items)
}

// worktreeRefs is the worktrees list as the selection sees it, each row grouped under its
// repository.
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

// worktreeFilterRows is the list's own rows with the name a search reads each by.
func worktreeFilterRows(rows []worktreeRow) []filterRow {
	return xslice.Map(rows, func(row worktreeRow) filterRow {
		return filterRow{key: row.key(), name: row.name()}
	})
}

// worktreesSnapshotParams is what the panel holds about the worktrees list, as plain data.
type worktreesSnapshotParams struct {
	rows     []worktreeRow
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
	for i, r := range params.rows {
		row := view.Row{
			Key:     r.key(),
			Name:    r.name(),
			Matches: matchesIn(params.matcher, r.name()),
		}

		switch {
		case r.header:
			row.Kind = view.KindWorkflow
			row.Process = view.ProcessNone
			row.Trail = groupTrail(params.rows[i+1:], r.repo)
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

// worktreeAgentFacts is what a row says about the agent behind it, read off the agents listing
// where it holds the agent and off the worktree listing where it does not yet.
func worktreeAgentFacts(row view.Row, agent worktreeAgent, live map[string]agentItem) view.Row {
	item, ok := live[agent.id]
	if !ok {
		item = agentItem{agent: agentstore.Agent{
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

// rowTrail is a directory's size then its age, the size a dash until it has been measured.
func rowTrail(item worktreeItem, now time.Time) string {
	return view.Trail(sizeLabel(item), dates.AgeAt(item.lastActiveAt, now))
}

// groupTrail is the total of the measured directories under a header, over the size column.
func groupTrail(rest []worktreeRow, repo string) string {
	var under []worktreeItem
	for _, r := range rest {
		if r.header || r.repo != repo {
			break
		}

		under = append(under, r.item)
	}

	return view.Trail(totalLabel(under), "")
}

// sizeLabel is what a directory takes, a dash until it has been measured or while its last
// walk failed.
func sizeLabel(item worktreeItem) string {
	if item.size == nil || item.size.err != "" {
		return label.Dash
	}

	return label.Bytes(item.size.bytes)
}

// totalLabel is what the measured directories take together, a dash while none is.
func totalLabel(items []worktreeItem) string {
	var total int64
	measured := false
	for _, item := range items {
		if item.size == nil || item.size.err != "" {
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

// shortenHome spells a path under the home directory with a tilde.
func shortenHome(path string) string {
	home := homeDir()
	if home == "" || path == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rel
	}

	return path
}
