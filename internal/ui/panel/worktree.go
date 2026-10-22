package panel

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"komrad/lib/dates"
	"komrad/lib/xslice"
	worktreev1 "komrad/proto/gen/worktree/v1"
)

// Worktree is one directory as the listing reported it.
type Worktree struct {
	Dir      string
	RepoDir  string
	Branch   string
	Readable bool
	// Task is the task the directory is the checkout of, nil when none answers for it.
	Task *Task
	// Occupants are the agents working in the directory, live ones first.
	Occupants []WorktreeAgent
	// Size is the last measurement, nil until the loop has reached the directory.
	Size *WorktreeSize
	// Standing is the last reading of git, nil until one has been taken.
	Standing     *WorktreeStanding
	LastActiveAt time.Time
	Fate         WorktreeFate
}

// WorktreeAgent is what the listing says of an agent that holds a directory.
type WorktreeAgent struct {
	ID         string
	Name       string
	State      State
	ArchivedAt *time.Time
}

// WorktreeSize is what a directory took when it was last walked, or why the walk failed.
type WorktreeSize struct {
	Bytes      int64
	Share      int64
	MeasuredAt time.Time
	// Err is why the last walk failed, empty when it did not.
	Err string
}

// WorktreeStanding is where a directory stood against its base when git was last asked, or why
// git could not say.
type WorktreeStanding struct {
	Dirty      bool
	Sequencing bool
	Unlanded   int
	// Err is why the last reading failed, empty when it did not.
	Err string
}

// WorktreeFate is which of the delete paths a directory takes.
type WorktreeFate int

const (
	// FateUnowned is a directory no task answers for, which the remover takes.
	FateUnowned WorktreeFate = iota
	// FateLive is a directory whose task is open, which is offered the task's archive.
	FateLive
	// FateArchived is a directory whose task is put away, which the task delete takes.
	FateArchived
	// FateDeleting is a directory whose task a delete has claimed, which nothing more is asked of.
	FateDeleting
)

// IsMeasured reports whether the directory carries a size.
func (w Worktree) IsMeasured() bool {
	return w.Size != nil && w.Size.Err == ""
}

// IsMerged reports whether the base holds everything the directory has, leaving nothing on disk.
func (w Worktree) IsMerged() bool {
	standing := w.Standing

	return standing != nil && standing.Err == "" && !standing.Dirty && !standing.Sequencing && standing.Unlanded == 0
}

// IsInactive reports whether no live agent answers for or works in the directory.
func (w Worktree) IsInactive() bool {
	if w.Fate == FateLive {
		return false
	}

	return !slices.ContainsFunc(w.Occupants, func(occupant WorktreeAgent) bool {
		return occupant.ArchivedAt == nil
	})
}

// HoldsAgent reports whether the agent works in the directory.
func (w Worktree) HoldsAgent(id string) bool {
	return slices.ContainsFunc(w.Occupants, func(occupant WorktreeAgent) bool { return occupant.ID == id })
}

// FirstOccupant is the occupant the row draws and jumps to, false when the directory has none.
func (w Worktree) FirstOccupant() (WorktreeAgent, bool) {
	if len(w.Occupants) == 0 {
		return WorktreeAgent{}, false
	}

	return w.Occupants[0], true
}

// worktreeHolders is every agent indexed by what ties it to a directory.
type worktreeHolders struct {
	// byTask holds the top-level agents provisioned for each task.
	byTask map[string][]Agent
	byCwd  map[string][]Agent
	tasks  Tasks
}

// worktreesFrom renders every directory in the world with its task and the agents working in it.
func worktreesFrom(worktrees map[string]*worktreev1.WatchedWorktree, agents []AgentRow, tasks Tasks) []Worktree {
	holders := worktreeHolders{
		byTask: map[string][]Agent{},
		tasks:  tasks,
		byCwd:  map[string][]Agent{},
	}
	for _, row := range agents {
		cwd := filepath.Clean(row.Agent.Cwd)
		holders.byCwd[cwd] = append(holders.byCwd[cwd], row.Agent)
		if row.Agent.TaskID != "" && row.Agent.isCheckoutOwner() {
			holders.byTask[row.Agent.TaskID] = append(holders.byTask[row.Agent.TaskID], row.Agent)
		}
	}

	items := make([]Worktree, 0, len(worktrees))
	for _, worktree := range worktrees {
		items = append(items, worktreeFromWatched(worktree, holders))
	}
	slices.SortFunc(items, func(a, b Worktree) int { return strings.Compare(a.Dir, b.Dir) })

	return items
}

func worktreeFromWatched(worktree *worktreev1.WatchedWorktree, holders worktreeHolders) Worktree {
	item := Worktree{
		Dir:      worktree.GetDir(),
		RepoDir:  worktree.GetRepoDir(),
		Branch:   worktree.GetBranch(),
		Readable: worktree.GetReadable(),
	}
	if worktree.MeasuredAt != nil {
		item.Size = &WorktreeSize{
			Bytes:      worktree.GetSizeBytes(),
			Share:      worktree.GetShareBytes(),
			MeasuredAt: dates.Parse(worktree.GetMeasuredAt()),
			Err:        worktree.GetSizeError(),
		}
	}
	if standing := worktree.GetStanding(); standing != nil {
		item.Standing = &WorktreeStanding{
			Dirty:      standing.GetDirty(),
			Sequencing: standing.GetSequencing(),
			Unlanded:   int(standing.GetUnlanded()),
			Err:        standing.GetError(),
		}
	}

	name := filepath.Base(item.Dir)
	if task, ok := holders.tasks[name]; ok {
		item.Task = &task
	}
	item.Fate = fateOf(item.Task)

	occupants := xslice.UniqueBy(slices.Concat(holders.byTask[name], holders.byCwd[item.Dir]), func(agent Agent) string { return agent.ID })
	slices.SortFunc(occupants, compareOccupants)
	item.Occupants = xslice.Map(occupants, worktreeAgentOf)
	item.LastActiveAt = lastActiveOf(occupants, dates.Parse(worktree.GetModifiedAt()))

	return item
}

func worktreeAgentOf(agent Agent) WorktreeAgent {
	return WorktreeAgent{
		ID:         agent.ID,
		Name:       agent.Name,
		State:      agent.State,
		ArchivedAt: agent.ArchivedAt,
	}
}

// fateOf is which delete path a directory takes, decided by the task it is the checkout of.
func fateOf(task *Task) WorktreeFate {
	switch {
	case task == nil:
		return FateUnowned
	case task.ArchivedAt != nil:
		return FateArchived
	}

	return FateLive
}

// lastActiveOf is when a directory was last touched, by its occupants when there are any and by
// its mtime otherwise.
func lastActiveOf(occupants []Agent, modifiedAt time.Time) time.Time {
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

// compareOccupants orders the agents in a directory, a live one before an archived one and then
// a newer one before an older.
func compareOccupants(a, b Agent) int {
	if (a.ArchivedAt == nil) != (b.ArchivedAt == nil) {
		if a.ArchivedAt == nil {
			return -1
		}

		return 1
	}

	return b.CreatedAt.Compare(a.CreatedAt)
}
