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

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	worktreestore "weave/internal/store/worktree"
	"weave/lib/xslice"
)

// Agent is what a listing says about an agent that holds a directory.
type Agent struct {
	ID             string
	Name           string
	State          agentstore.State
	StateChangedAt time.Time
	ArchivedAt     *time.Time
	// Adopted says the branch was somebody's before the agent was put on it.
	Adopted bool
	// FailedToCleanup says the agent's own delete refused the directory.
	FailedToCleanup bool
	// Deleting says a delete has claimed the agent and not yet taken it.
	Deleting   bool
	WorkflowID string
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
	// Agent is the row the directory was made for, nil when none answers.
	Agent *Agent
	// Occupants are the other rows working in this directory.
	Occupants []Agent
	// ModifiedAt is when git last wrote the directory's .git file, else the directory's own
	// modification time, zero when neither could be read.
	ModifiedAt time.Time
	// Size is the directory's last measurement, nil until the loop has been round.
	Size *worktreestore.Size
	// LastActiveAt is when the directory was last touched, read off its agent when one answers
	// and off the filesystem otherwise.
	LastActiveAt time.Time
}

// Fate is which delete path a directory takes.
type Fate int

const (
	// FateUnowned is a directory nothing answers for, which the remover takes.
	FateUnowned Fate = iota
	// FateArchived is a directory whose agent is put away, which the agent delete takes.
	FateArchived
	// FateLive is a directory whose agent is running, which nothing takes.
	FateLive
)

// Fate is which delete path the directory takes.
func (w Worktree) Fate() Fate {
	switch {
	case w.Agent == nil:
		return FateUnowned
	case w.Agent.FailedToCleanup && !w.Readable:
		return FateUnowned
	case w.Agent.ArchivedAt != nil:
		return FateArchived
	}

	return FateLive
}

// Listing is every directory under the worktree directories.
type Listing struct {
	Worktrees []Worktree
	// Dirs are the directories that were walked.
	Dirs []string
	// UnreadableDirs are those of them that could not be read.
	UnreadableDirs []string
}

type agentDirLister interface {
	ListDirs(ctx context.Context) ([]agentstore.AgentDir, error)
}

type worktreeDirLister interface {
	ListWorktreeDirs(ctx context.Context) ([]string, error)
}

type sizeLister interface {
	ListSizes(ctx context.Context) ([]worktreestore.Size, error)
}

type NewListerParams struct {
	// SharedDir reads the configured worktrees directory, resolved.
	SharedDir func() (string, error)
	Agents    agentDirLister
	Workflows worktreeDirLister
	Sizes     sizeLister
}

// Lister reads the worktree directories and the rows that answer for them.
type Lister struct {
	shared    func() (string, error)
	agents    agentDirLister
	workflows worktreeDirLister
	sizes     sizeLister
}

// NewLister builds a lister over the stores.
func NewLister(params NewListerParams) *Lister {
	return &Lister{
		shared:    params.SharedDir,
		agents:    params.Agents,
		workflows: params.Workflows,
		sizes:     params.Sizes,
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
	owners, err := l.agents.ListDirs(ctx)
	if err != nil {
		return nil, err
	}

	sizes, err := l.sizes.ListSizes(ctx)
	if err != nil {
		return nil, err
	}
	sized := xslice.KeyBy(sizes, func(size worktreestore.Size) string { return size.Dir })

	return func(dir string) Worktree {
		worktree := read(dir)
		worktree.Agent, worktree.Occupants = owned(dir, owners)
		if size, ok := sized[dir]; ok {
			worktree.Size = &size
		}
		worktree.LastActiveAt = lastActive(worktree)

		return worktree
	}, nil
}

// Paths is every directory found immediately under the worktree directories.
type Paths struct {
	Paths []string
	// Unreadable are the worktree directories that could not be read.
	Unreadable []string
}

func walk(dirs []string) Paths {
	found := Paths{}
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

// lastActive is when a directory was last touched, by its agent when one answers and by its
// mtime otherwise.
func lastActive(worktree Worktree) time.Time {
	agent := worktree.Agent
	if agent == nil {
		return worktree.ModifiedAt
	}
	if agent.ArchivedAt != nil && agent.ArchivedAt.After(agent.StateChangedAt) {
		return *agent.ArchivedAt
	}

	return agent.StateChangedAt
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

// owned is the row a directory was made for, and every other row working in it.
func owned(dir string, owners []agentstore.AgentDir) (*Agent, []Agent) {
	var owner *Agent
	var occupants []Agent

	name := filepath.Base(dir)
	for _, row := range owners {
		switch {
		case row.ID == name:
			owner = describe(row)
		case filepath.Clean(row.Cwd) == dir:
			occupants = append(occupants, *describe(row))
		}
	}

	slices.SortFunc(occupants, func(a, b Agent) int { return strings.Compare(a.Name, b.Name) })

	return owner, occupants
}

func describe(row agentstore.AgentDir) *Agent {
	return &Agent{
		ID:              row.ID,
		Name:            row.Name,
		State:           row.State,
		StateChangedAt:  row.StateChangedAt,
		ArchivedAt:      row.ArchivedAt,
		Adopted:         row.AdoptedBranch != "",
		FailedToCleanup: row.State == agentstore.StateFailedToCleanup,
		Deleting:        row.DeletingAt != nil,
		WorkflowID:      row.WorkflowID,
	}
}

// Unclaimed drops the directories a delete has claimed: those whose agent is being deleted, and
// those nothing answers for that an unfinished delete names.
func Unclaimed(listing []Worktree, claimed []string) []Worktree {
	named := make(map[string]bool, len(claimed))
	for _, dir := range claimed {
		named[dir] = true
	}

	return xslice.Filter(listing, func(worktree Worktree) bool {
		switch {
		case worktree.Agent != nil && worktree.Agent.Deleting:
			return false
		case worktree.Fate() == FateUnowned:
			return !named[worktree.Dir]
		}

		return true
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
