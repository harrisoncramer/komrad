package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"komrad/db"
	"komrad/internal/git"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5/pgxpool"
)

const connectTimeout = 5 * time.Second

// writeAccess is the mode syscall.Access takes for a write, which the syscall package has no
// name of its own for.
const writeAccess = 0x2

func pingDatabase(ctx context.Context, url string) error {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return err
	}
	pool.Close()

	return nil
}

func readPendingMigrations(ctx context.Context, url string) ([]string, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	return db.Pending(ctx, pool)
}

func openReadOnlyPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to read the database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, err
	}

	return pool, nil
}

// connectReadOnly opens the read-only pool as the service does, paired with the engine databaseURL logs in as.
func connectReadOnly(ctx context.Context, url, databaseURL string) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	engine, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = engine.Close() }()

	pool, err := store.OpenReadOnly(ctx, url, engine.GetEngine().ID)
	if err != nil {
		return err
	}
	pool.Close()

	return nil
}

// readEngine names the engine the login in url belongs to, opening the store the way the service does.
func readEngine(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := store.Open(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = pool.Close() }()

	return pool.GetEngine().Name, nil
}

// readWorkflowBases reads the directory and base branch of every live workflow.
func readWorkflowBases(ctx context.Context, url string) ([]WorkflowBase, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	workflows, err := workflowstore.NewWorkflowStore(pool).List(ctx)
	if err != nil {
		return nil, err
	}

	return xslice.Map(workflows, func(workflow workflowstore.Workflow) WorkflowBase {
		return WorkflowBase{
			Name: workflow.Name,
			Cwd:  workflow.Cwd,
			Base: workflow.Worktree.BaseBranch,
		}
	}), nil
}

func readWorkflowHarnesses(ctx context.Context, url string) ([]WorkflowHarness, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	workflows, err := workflowstore.NewWorkflowStore(pool).List(ctx)
	if err != nil {
		return nil, err
	}

	return xslice.Map(workflows, func(workflow workflowstore.Workflow) WorkflowHarness {
		return WorkflowHarness{
			Name:    workflow.Name,
			Cwd:     workflow.Cwd,
			Harness: workflow.Harness,
		}
	}), nil
}

// resolveWorkflowBase is the ref a workflow's clone holds for its base, read without a fetch.
func resolveWorkflowBase(ctx context.Context, dir, base string) (string, error) {
	client := git.NewClient()

	root, err := client.GetRoot(ctx, dir)
	if err != nil {
		return "", err
	}

	return client.ResolveBase(ctx, root, base)
}

// inspectDir is what a directory looks like from outside: whether it is there, whether it is
// a directory at all, whether komrad could put something in it, and what it holds.
func inspectDir(path string) (DirState, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DirState{}, nil
	}
	if err != nil {
		return DirState{}, err
	}
	if !info.IsDir() {
		return DirState{Present: true}, nil
	}

	state := DirState{
		Present:   true,
		Directory: true,
		Writable:  syscall.Access(path, writeAccess) == nil,
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return state, err
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if entry.IsDir() {
			state.Directories = append(state.Directories, entry.Name())

			continue
		}

		state.Files = append(state.Files, entry.Name())
	}

	return state, nil
}

// readWorktreeOwners reads who could own a worktree directory: every agent, archived ones
// included, and the directories the workflows cut their worktrees in.
func readWorktreeOwners(ctx context.Context, url string) (WorktreeOwners, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return WorktreeOwners{}, err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	agents, err := agentstore.NewAgentStore(pool).ListDirs(ctx)
	if err != nil {
		return WorktreeOwners{}, err
	}

	var owners WorktreeOwners
	for _, agent := range agents {
		owners.AgentIDs = append(owners.AgentIDs, agent.ID)
		if trimmed := strings.TrimSpace(agent.Cwd); trimmed != "" {
			owners.AgentDirs = append(owners.AgentDirs, trimmed)
		}
	}

	owners.WorkflowDirs, err = workflowstore.NewWorkflowStore(pool).ListWorktreeDirs(ctx)
	if err != nil {
		return WorktreeOwners{}, err
	}

	return owners, nil
}
