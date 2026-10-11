package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/panel"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	worktreev1 "komrad/proto/gen/worktree/v1"
)

// worktreeAgent is what the listing says of an agent that holds a directory.
type worktreeAgent struct {
	id         string
	name       string
	state      panel.State
	archivedAt *time.Time
}

// worktreeSize is what a directory took when it was last walked, or why the walk failed.
type worktreeSize struct {
	bytes      int64
	share      int64
	measuredAt time.Time
	// err is why the last walk failed, empty when it did not.
	err string
}

// worktreeStanding is where a directory stood against its base when git was last asked, or why
// git could not say.
type worktreeStanding struct {
	dirty      bool
	sequencing bool
	unlanded   int
	// err is why the last reading failed, empty when it did not.
	err string
}

// worktreeItem is one directory as the listing reported it.
type worktreeItem struct {
	dir      string
	repoDir  string
	branch   string
	readable bool
	// task is the task the directory is the checkout of, nil when none answers for it.
	task *panel.Task
	// occupants are the agents working in the directory, live ones first.
	occupants []worktreeAgent
	// size is the last measurement, nil until the loop has reached the directory.
	size *worktreeSize
	// standing is the last reading of git, nil until one has been taken.
	standing     *worktreeStanding
	lastActiveAt time.Time
	fate         worktreeFate
}

// isMeasured reports whether the directory carries a size.
func (i worktreeItem) isMeasured() bool {
	return i.size != nil && i.size.err == ""
}

// isMerged reports whether the base holds everything the directory has, leaving nothing on disk.
func (i worktreeItem) isMerged() bool {
	standing := i.standing

	return standing != nil && standing.err == "" && !standing.dirty && !standing.sequencing && standing.unlanded == 0
}

// firstOccupant is the occupant the row draws and jumps to, false when the directory has none.
func (i worktreeItem) firstOccupant() (worktreeAgent, bool) {
	if len(i.occupants) == 0 {
		return worktreeAgent{}, false
	}

	return i.occupants[0], true
}

// worktreeHolders is every agent indexed by what ties it to a directory.
type worktreeHolders struct {
	// byTask holds the top-level agents provisioned for each task.
	byTask map[string][]panel.Agent
	byCwd  map[string][]panel.Agent
	tasks  map[string]panel.Task
}

// worktreeItemsFrom renders every directory in the world with its task and the agents working in it.
func worktreeItemsFrom(worktrees map[string]*worktreev1.WatchedWorktree, agents []panel.AgentRow, tasks map[string]panel.Task) []worktreeItem {
	holders := worktreeHolders{
		byTask: map[string][]panel.Agent{},
		tasks:  tasks,
		byCwd:  map[string][]panel.Agent{},
	}
	for _, row := range agents {
		cwd := filepath.Clean(row.Agent.Cwd)
		holders.byCwd[cwd] = append(holders.byCwd[cwd], row.Agent)
		if row.Agent.TaskID != "" && isCheckoutOwner(row.Agent) {
			holders.byTask[row.Agent.TaskID] = append(holders.byTask[row.Agent.TaskID], row.Agent)
		}
	}

	items := make([]worktreeItem, 0, len(worktrees))
	for _, worktree := range worktrees {
		items = append(items, worktreeItemFromWatched(worktree, holders))
	}
	slices.SortFunc(items, func(a, b worktreeItem) int { return strings.Compare(a.dir, b.dir) })

	return items
}

func worktreeItemFromWatched(worktree *worktreev1.WatchedWorktree, holders worktreeHolders) worktreeItem {
	item := worktreeItem{
		dir:      worktree.GetDir(),
		repoDir:  worktree.GetRepoDir(),
		branch:   worktree.GetBranch(),
		readable: worktree.GetReadable(),
	}
	if worktree.MeasuredAt != nil {
		item.size = &worktreeSize{
			bytes:      worktree.GetSizeBytes(),
			share:      worktree.GetShareBytes(),
			measuredAt: dates.Parse(worktree.GetMeasuredAt()),
			err:        worktree.GetSizeError(),
		}
	}
	if standing := worktree.GetStanding(); standing != nil {
		item.standing = &worktreeStanding{
			dirty:      standing.GetDirty(),
			sequencing: standing.GetSequencing(),
			unlanded:   int(standing.GetUnlanded()),
			err:        standing.GetError(),
		}
	}

	name := filepath.Base(item.dir)
	if task, ok := holders.tasks[name]; ok {
		item.task = &task
	}
	item.fate = fateOf(item.task)

	occupants := xslice.UniqueBy(slices.Concat(holders.byTask[name], holders.byCwd[item.dir]), func(agent panel.Agent) string { return agent.ID })
	slices.SortFunc(occupants, compareOccupants)
	item.occupants = xslice.Map(occupants, worktreeAgentOf)
	item.lastActiveAt = lastActiveOf(occupants, dates.Parse(worktree.GetModifiedAt()))

	return item
}

func worktreeAgentOf(agent panel.Agent) worktreeAgent {
	return worktreeAgent{
		id:         agent.ID,
		name:       agent.Name,
		state:      agent.State,
		archivedAt: agent.ArchivedAt,
	}
}

// fateOf is which delete path a directory takes, decided by the task it is the checkout of.
func fateOf(task *panel.Task) worktreeFate {
	switch {
	case task == nil:
		return fateUnowned
	case task.ArchivedAt != nil:
		return fateArchived
	}

	return fateLive
}

// lastActiveOf is when a directory was last touched, by its occupants when there are any and by
// its mtime otherwise.
func lastActiveOf(occupants []panel.Agent, modifiedAt time.Time) time.Time {
	if len(occupants) == 0 {
		return modifiedAt
	}

	var latest time.Time
	for _, agent := range occupants {
		touched := agent.StateChangedAt
		if agent.ArchivedAt != nil && agent.ArchivedAt.After(touched) {
			touched = *agent.ArchivedAt
		}
		if touched.After(latest) {
			latest = touched
		}
	}

	return latest
}

// worktreeEntry is one top-level entry of a directory.
type worktreeEntry struct {
	path  string
	bytes int64
	dir   bool
}

// worktreeCommit is one of a branch's commits since its base.
type worktreeCommit struct {
	sha         string
	subject     string
	committedAt time.Time
}

// worktreeInspection is one directory read through git, or why it could not be.
type worktreeInspection struct {
	// pending says a fresh reading is on its way, and answered that one has come at all.
	pending  bool
	answered bool
	// readAt is when the answer came.
	readAt time.Time
	// item is the directory as the listing reports it, measured afresh by the read.
	item      worktreeItem
	entries   []worktreeEntry
	modified  []string
	untracked []string
	baseRef   string
	// baseError is why git could not name a base, when baseRef is empty for that reason.
	baseError string
	ahead     int
	behind    int
	pushed    bool
	unpushed  int
	unpulled  int
	// unpulledLanded says the base already holds every unpulled commit.
	unpulledLanded bool
	// landed is how many of the branch's own commits the base already holds whole.
	landed int
	// sequencing says a merge, a rebase or a cherry-pick is part-way through.
	sequencing  bool
	keepsBranch bool
	commits     []worktreeCommit
	err         error
}

// isStale reports whether the answer is old enough to ask for again.
func (r worktreeInspection) isStale(now time.Time) bool {
	return !r.answered || (!r.isSettled() && now.Sub(r.readAt) > worktreeReadTTL)
}

// isRead reports whether git has answered for the directory.
func (r worktreeInspection) isRead() bool {
	return r.answered && (r.err == nil || r.item.dir != "")
}

// isSettled reports whether the answer is one that will never change.
func (r worktreeInspection) isSettled() bool {
	return r.answered && errui.IsNotFound(r.err)
}

// isInactive reports whether no live agent answers for or works in the directory.
func (i worktreeItem) isInactive() bool {
	if i.fate == fateLive {
		return false
	}

	return !slices.ContainsFunc(i.occupants, func(occupant worktreeAgent) bool {
		return occupant.archivedAt == nil
	})
}

// holdsAgent reports whether the agent works in the directory.
func (i worktreeItem) holdsAgent(id string) bool {
	return slices.ContainsFunc(i.occupants, func(occupant worktreeAgent) bool { return occupant.id == id })
}

// worktreeScope is which of the directories the worktrees screen lists.
type worktreeScope int

const (
	scopeAll worktreeScope = iota
	scopeInactive
	scopeMerged
)

var worktreeScopeLabels = []string{"all", "inactive", "merged"}

// holds reports whether the scope lists the directory.
func (s worktreeScope) holds(item worktreeItem) bool {
	switch s {
	case scopeInactive:
		return item.isInactive()
	case scopeMerged:
		return item.isInactive() && item.isMerged()
	case scopeAll:
	}

	return true
}

// listsAgent reports whether the scope lists a directory the agent holds.
func (s worktreeScope) listsAgent(items []worktreeItem, id string) bool {
	return slices.ContainsFunc(items, func(item worktreeItem) bool { return s.holds(item) && item.holdsAgent(id) })
}

// worktreesScreen is the worktrees screen: the directories the listing holds, the cursor in
// them, which of them are listed, and how they are ordered.
type worktreesScreen struct {
	rowCursor
	items      []worktreeItem
	scope      worktreeScope
	scopeFocus bool
	sort       tabsState
	revision   int
	built      derived[worktreeRowsKey, []worktreeRow]
}

// apply takes a fresh listing, keeping the cursor on the row it was on, or on the agent the
// screen was opened for.
func (w *worktreesScreen) apply(m *model, items []worktreeItem, hovered string) {
	w.items = items
	w.revision++

	focus := w.focus
	w.focus = ""

	rows := w.rows(m)
	if idx := rowIndexOfWorktreeAgent(rows, focus); idx >= 0 {
		hovered = rows[idx].key()
	}

	w.place(m, hovered)
}

// shown is the directories the scope lists.
func (w *worktreesScreen) shown() []worktreeItem {
	return xslice.Filter(w.items, w.scope.holds)
}

// place keeps the cursor on the row it was on as the rows under it change.
func (w *worktreesScreen) place(m *model, hovered string) {
	rows := w.rows(m)
	w.selected = w.selected.pruned(worktreeRefs(rows))

	w.cursor = cursor.Place(rowIndexOfKey(rows, hovered), w.cursor, len(rows))
}

// compareOccupants orders the agents in a directory, a live one before an archived one and then
// a newer one before an older.
func compareOccupants(a, b panel.Agent) int {
	if (a.ArchivedAt == nil) != (b.ArchivedAt == nil) {
		if a.ArchivedAt == nil {
			return -1
		}

		return 1
	}

	return b.CreatedAt.Compare(a.CreatedAt)
}
