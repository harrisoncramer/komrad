package worktrees

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
	"komrad/lib/xslice"
)

// Agent is what a listing says about an agent working in a directory.
type Agent struct {
	ID             string
	Name           string
	State          agentstore.State
	StateChangedAt time.Time
	ArchivedAt     *time.Time
}

// Task is what a listing says about the task whose checkout a directory is.
type Task struct {
	ID         string
	WorkflowID string
	Slug       string
	// Adopted says the branch was somebody's before the task was put on it.
	Adopted    bool
	ArchivedAt *time.Time
	// Deleting says a delete has claimed the task and not yet taken it.
	Deleting bool
}

// Worktree is one directory under the worktree directories, and what answers for it.
type Worktree struct {
	Dir string
	// RepoDir is the repository the directory was cut from, read off its .git file, and empty
	// when there is no such file.
	RepoDir string
	// Branch is what the gitdir's HEAD names, empty when it is detached or cannot be read.
	Branch string
	// Readable says the gitdir the .git file names is still there.
	Readable bool
	// Locked says git holds a lock on the directory's registration.
	Locked bool
	// Task is the task whose checkout the directory is, nil when none answers.
	Task *Task
	// Occupants are the agents working in the directory, live ones first.
	Occupants []Agent
	// ModifiedAt is when git last wrote the directory's .git file, else the directory's own
	// modification time, zero when neither could be read.
	ModifiedAt time.Time
	// Size is the directory's last measurement, nil until the loop has been round.
	Size *worktreestore.Size
	// LastActiveAt is when the directory was last touched, read off its occupants when there are
	// any and off the filesystem otherwise.
	LastActiveAt time.Time
}

// Fate is which delete path a directory takes.
type Fate int

const (
	// FateUnowned is a directory no task answers for, which the remover takes.
	FateUnowned Fate = iota
	// FateLive is a directory whose task is open, which nothing takes.
	FateLive
	// FateArchived is a directory whose task is put away, which the task delete takes.
	FateArchived
	// FateDeleting is a directory whose task a delete has claimed, which that delete takes.
	FateDeleting
)

// Fate is which delete path the directory takes.
func (w Worktree) Fate() Fate {
	switch {
	case w.Task == nil:
		return FateUnowned
	case w.Task.Deleting:
		return FateDeleting
	case w.Task.ArchivedAt != nil:
		return FateArchived
	}

	return FateLive
}

// Listing is every directory under the worktree directories.
type Listing struct {
	Worktrees []Worktree
	// Dirs are the directories the walk covered.
	Dirs []string
	// UnreadableDirs are those of them that could not be read.
	UnreadableDirs []string
}

type NewListerParams struct {
	// SharedDir reads the configured worktrees directory, resolved.
	SharedDir func() (string, error)
	Agents    *agentstore.AgentStore
	Tasks     *taskstore.TaskStore
	Workflows *workflowstore.WorkflowStore
	Worktrees *worktreestore.WorktreeStore
	Claimed   claimedDirLister
}

// Lister reads the worktree directories and the rows that answer for them.
type Lister struct {
	shared    func() (string, error)
	agents    *agentstore.AgentStore
	tasks     *taskstore.TaskStore
	workflows *workflowstore.WorkflowStore
	worktrees *worktreestore.WorktreeStore
	claimed   claimedDirLister
}

// NewLister builds a lister over the stores.
func NewLister(params NewListerParams) *Lister {
	return &Lister{
		shared:    params.SharedDir,
		agents:    params.Agents,
		tasks:     params.Tasks,
		workflows: params.Workflows,
		worktrees: params.Worktrees,
		claimed:   params.Claimed,
	}
}

// ListDirs is every directory worktrees are cut into, the shared one and each workflow's own.
func (l *Lister) ListDirs(ctx context.Context) ([]string, error) {
	shared, err := l.shared()
	if err != nil {
		return nil, err
	}

	own, err := l.workflows.ListWorktreeDirs(ctx)
	if err != nil {
		return nil, err
	}

	dirs := []string{filepath.Clean(shared)}
	for _, dir := range own {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			dirs = append(dirs, filepath.Clean(config.ExpandHome(trimmed)))
		}
	}

	return xslice.UniqueBy(dirs, func(dir string) string { return dir }), nil
}

// List is every directory under the worktree directories, with what answers for each.
func (l *Lister) List(ctx context.Context) (*Listing, error) {
	dirs, err := l.ListDirs(ctx)
	if err != nil {
		return nil, err
	}

	answer, err := l.answerer(ctx)
	if err != nil {
		return nil, err
	}

	found := walk(dirs)

	return &Listing{
		Dirs:           dirs,
		UnreadableDirs: found.Unreadable,
		Worktrees:      xslice.Map(found.Paths, answer),
	}, nil
}

// ListUnclaimed is the listing without the directories a delete has claimed.
func (l *Lister) ListUnclaimed(ctx context.Context) (*Listing, error) {
	listing, err := l.List(ctx)
	if err != nil {
		return nil, err
	}

	claimed, err := l.claimed.ListClaimedDirs(ctx)
	if err != nil {
		return nil, err
	}

	listing.Worktrees = unclaimed(listing.Worktrees, claimed)

	return listing, nil
}

// Get is one directory under the worktree directories, with what answers for it.
func (l *Lister) Get(ctx context.Context, dir string) (*Worktree, error) {
	dir = filepath.Clean(dir)

	dirs, err := l.ListDirs(ctx)
	if err != nil {
		return nil, err
	}

	parent := filepath.Dir(dir)
	if !slices.Contains(dirs, parent) || !slices.Contains(walk([]string{parent}).Paths, dir) {
		return nil, store.ErrNotFound.Saying("%s is not under the worktree directories", dir)
	}

	answer, err := l.answerer(ctx)
	if err != nil {
		return nil, err
	}
	worktree := answer(dir)

	return &worktree, nil
}

// answerer reads the rows and the sizes once and describes any directory against them.
func (l *Lister) answerer(ctx context.Context) (func(dir string) Worktree, error) {
	rows, err := l.agents.ListDirs(ctx)
	if err != nil {
		return nil, err
	}

	summaries, err := l.tasks.ListSummaries(ctx)
	if err != nil {
		return nil, err
	}
	tasks := xslice.KeyBy(summaries, func(summary taskstore.Summary) string { return summary.ID })

	sizes, err := l.worktrees.ListSizes(ctx)
	if err != nil {
		return nil, err
	}
	sized := xslice.KeyBy(sizes, func(size worktreestore.Size) string { return size.Dir })

	return func(dir string) Worktree {
		worktree := read(dir)
		if summary, ok := tasks[filepath.Base(dir)]; ok {
			worktree.Task = describeTask(summary)
		}
		worktree.Occupants = listOccupants(dir, rows)
		if size, ok := sized[dir]; ok {
			worktree.Size = &size
		}
		worktree.LastActiveAt = lastActive(worktree)

		return worktree
	}, nil
}

// paths is every directory found immediately under the worktree directories.
type paths struct {
	Paths []string
	// Unreadable are the worktree directories that could not be read.
	Unreadable []string
}

func walk(dirs []string) paths {
	found := paths{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			found.Unreadable = append(found.Unreadable, dir)

			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				found.Paths = append(found.Paths, filepath.Join(dir, entry.Name()))
			}
		}
	}

	return found
}

// lastActive is when a directory was last touched, by its occupants when there are any and by
// its mtime otherwise.
func lastActive(worktree Worktree) time.Time {
	if len(worktree.Occupants) == 0 {
		return worktree.ModifiedAt
	}

	var latest time.Time
	for _, agent := range worktree.Occupants {
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

// modifiedAt is the mtime of a directory's .git file, else its own.
func modifiedAt(dir string) time.Time {
	for _, path := range []string{filepath.Join(dir, ".git"), dir} {
		if info, err := os.Lstat(path); err == nil {
			return info.ModTime()
		}
	}

	return time.Time{}
}

// read is what a directory says about itself without asking git.
func read(dir string) Worktree {
	worktree := Worktree{Dir: dir, ModifiedAt: modifiedAt(dir)}

	gitdir := readGitdir(dir)
	if gitdir == "" {
		return worktree
	}

	worktree.RepoDir = repoOf(gitdir)

	info, err := os.Stat(gitdir)
	if err != nil || !info.IsDir() {
		return worktree
	}
	worktree.Readable = true
	worktree.Locked = isLocked(gitdir)

	worktree.Branch = readBranch(gitdir)

	return worktree
}

// readGitdir is the path a worktree's .git file names, empty when the file is not one.
func readGitdir(dir string) string {
	content, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return ""
	}

	line, _, _ := bytes.Cut(content, []byte("\n"))
	path, ok := bytes.CutPrefix(line, []byte("gitdir: "))
	if !ok {
		return ""
	}

	return strings.TrimSpace(string(path))
}

// repoOf is the repository a gitdir belongs to, which git keeps as <repo>/.git/worktrees/<name>.
func repoOf(gitdir string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(gitdir)))
}

// isLocked reports whether git has locked the registration a gitdir holds.
func isLocked(gitdir string) bool {
	_, err := os.Stat(filepath.Join(gitdir, "locked"))

	return err == nil
}

// readBranch is the branch a gitdir's HEAD names, empty for a detached head.
func readBranch(gitdir string) string {
	content, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return ""
	}

	ref, ok := strings.CutPrefix(strings.TrimSpace(string(content)), "ref: refs/heads/")
	if !ok {
		return ""
	}

	return ref
}

// listOccupants is every agent working in a directory, those provisioned for its task included,
// live ones first and then newer before older.
func listOccupants(dir string, rows []agentstore.AgentDir) []Agent {
	name := filepath.Base(dir)
	working := xslice.Filter(rows, func(row agentstore.AgentDir) bool {
		return row.ProvisionedTaskID == name || filepath.Clean(row.Cwd) == dir
	})
	slices.SortFunc(working, compareOccupants)

	return xslice.Map(working, describeAgent)
}

// compareOccupants orders the agents in a directory, a live one before an archived one and then
// a newer one before an older.
func compareOccupants(a, b agentstore.AgentDir) int {
	if (a.ArchivedAt == nil) != (b.ArchivedAt == nil) {
		if a.ArchivedAt == nil {
			return -1
		}

		return 1
	}

	return b.CreatedAt.Compare(a.CreatedAt)
}

func describeAgent(row agentstore.AgentDir) Agent {
	return Agent{
		ID:             row.ID,
		Name:           row.Name,
		State:          row.State,
		StateChangedAt: row.StateChangedAt,
		ArchivedAt:     row.ArchivedAt,
	}
}

func describeTask(summary taskstore.Summary) *Task {
	return &Task{
		ID:         summary.ID,
		WorkflowID: summary.WorkflowID,
		Slug:       summary.Slug,
		Adopted:    summary.Adopted,
		ArchivedAt: summary.ArchivedAt,
		Deleting:   summary.Deleting,
	}
}

// unclaimed drops the directories a delete has claimed: those whose task is being deleted, and
// those no task owns that an unfinished delete names.
func unclaimed(listing []Worktree, claimed []string) []Worktree {
	return xslice.Filter(unnamed(listing, claimed), func(worktree Worktree) bool {
		return worktree.Fate() != FateDeleting
	})
}

// unnamed drops the directories nothing answers for that an unfinished delete names.
func unnamed(listing []Worktree, claimed []string) []Worktree {
	named := make(map[string]bool, len(claimed))
	for _, dir := range claimed {
		named[dir] = true
	}

	return xslice.Filter(listing, func(worktree Worktree) bool {
		return worktree.Fate() != FateUnowned || !named[worktree.Dir]
	})
}

// isUnder reports whether a path is a directory immediately under one of the worktree
// directories, comparing the two with their symlinks resolved.
func isUnder(dirs []string, path string) (bool, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, fmt.Errorf("failed to resolve %q: %w", path, err)
	}

	for _, dir := range dirs {
		parent, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}

		if filepath.Dir(resolved) == parent {
			return true, nil
		}
	}

	return false, nil
}
