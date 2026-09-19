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
	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"
)

// Agent is what a listing says about an agent that holds a directory.
type Agent struct {
	ID         string
	Name       string
	State      agentstore.State
	ArchivedAt *time.Time
	// Adopted says the branch was somebody's before the agent was put on it.
	Adopted bool
	// FailedToCleanup says the agent's own delete refused the directory.
	FailedToCleanup bool
	WorkflowID      string
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

// Lister reads the worktree directories and the rows that answer for them.
type Lister struct {
	shared    func() (string, error)
	agents    agentDirLister
	workflows worktreeDirLister
}

type NewListerParams struct {
	// SharedDir reads the configured worktrees directory, resolved.
	SharedDir func() (string, error)
	Agents    agentDirLister
	Workflows worktreeDirLister
}

// NewLister builds a lister over the stores.
func NewLister(params NewListerParams) *Lister {
	return &Lister{
		shared:    params.SharedDir,
		agents:    params.Agents,
		workflows: params.Workflows,
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

	dirs := []string{shared}
	for _, dir := range own {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			dirs = append(dirs, config.ExpandHome(trimmed))
		}
	}

	return xslice.UniqueBy(dirs, func(dir string) string { return dir }), nil
}

// List is every directory under the worktree directories, with what answers for each.
func (l *Lister) List(ctx context.Context) (Listing, error) {
	dirs, err := l.ListDirs(ctx)
	if err != nil {
		return Listing{}, err
	}

	owners, err := l.agents.ListDirs(ctx)
	if err != nil {
		return Listing{}, err
	}

	found := walk(dirs)
	listing := Listing{Dirs: dirs, UnreadableDirs: found.unreadable}
	for _, dir := range found.paths {
		worktree := read(dir)
		worktree.Agent, worktree.Occupants = owned(dir, owners)

		listing.Worktrees = append(listing.Worktrees, worktree)
	}

	return listing, nil
}

// walked is every directory found immediately under the worktree directories.
type walked struct {
	paths      []string
	unreadable []string
}

func walk(dirs []string) walked {
	found := walked{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			found.unreadable = append(found.unreadable, dir)

			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				found.paths = append(found.paths, filepath.Join(dir, entry.Name()))
			}
		}
	}

	return found
}

// read is what a directory says about itself without asking git.
func read(dir string) Worktree {
	worktree := Worktree{Dir: dir}

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
		ArchivedAt:      row.ArchivedAt,
		Adopted:         row.AdoptedBranch != "",
		FailedToCleanup: row.State == agentstore.StateFailedToCleanup,
		WorkflowID:      row.WorkflowID,
	}
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
