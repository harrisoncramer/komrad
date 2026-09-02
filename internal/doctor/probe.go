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

	"weave/db"
	"weave/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

const connectTimeout = 5 * time.Second

// writeAccess is the mode syscall.Access takes for a write, which the syscall package has no
// name of its own for.
const writeAccess = 0x2

const (
	listAgentOwners          = `SELECT id::text, cwd FROM agents`
	listWorkflowWorktreeDirs = `SELECT DISTINCT provisioners.worktree_dir
FROM workflows
JOIN workflow_provisioner_configs AS provisioners ON provisioners.id = workflows.provisioner_config_id
WHERE btrim(provisioners.worktree_dir) <> ''`
)

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

func connectReadOnly(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := store.OpenReadOnly(ctx, url)
	if err != nil {
		return err
	}
	pool.Close()

	return nil
}

// inspectDir is what a directory looks like from outside: whether it is there, whether it is
// a directory at all, whether weave could put something in it, and what it holds.
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
// included since a dormant agent is restored into the tree it still holds, and the
// directories the workflows cut their worktrees in.
func readWorktreeOwners(ctx context.Context, url string) (WorktreeOwners, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return WorktreeOwners{}, err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	var owners WorktreeOwners

	rows, err := pool.Query(ctx, listAgentOwners)
	if err != nil {
		return WorktreeOwners{}, err
	}
	for rows.Next() {
		var id, cwd string
		if err := rows.Scan(&id, &cwd); err != nil {
			rows.Close()

			return WorktreeOwners{}, err
		}

		owners.AgentIDs = append(owners.AgentIDs, id)
		if trimmed := strings.TrimSpace(cwd); trimmed != "" {
			owners.AgentDirs = append(owners.AgentDirs, trimmed)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return WorktreeOwners{}, err
	}

	dirs, err := pool.Query(ctx, listWorkflowWorktreeDirs)
	if err != nil {
		return WorktreeOwners{}, err
	}
	defer dirs.Close()

	for dirs.Next() {
		var dir string
		if err := dirs.Scan(&dir); err != nil {
			return WorktreeOwners{}, err
		}

		owners.WorkflowDirs = append(owners.WorkflowDirs, dir)
	}

	return owners, dirs.Err()
}
