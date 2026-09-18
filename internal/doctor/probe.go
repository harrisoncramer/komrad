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

	"weave/config"
	"weave/db"
	"weave/internal/forge"
	"weave/internal/forge/github"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	forgestore "weave/internal/store/forge"
	workflowstore "weave/internal/store/workflow"

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
// included, and the directories the workflows cut their worktrees in.
func readWorktreeOwners(ctx context.Context, url string) (WorktreeOwners, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return WorktreeOwners{}, err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	agents, err := agentstore.New(pool).ListDirs(ctx)
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

	owners.WorkflowDirs, err = workflowstore.New(pool).ListWorktreeDirs(ctx)
	if err != nil {
		return WorktreeOwners{}, err
	}

	return owners, nil
}

// verifyForges asks each connected forge who weave is there, writing nothing back.
func verifyForges(ctx context.Context, url string) ([]ForgeStatus, error) {
	pool, err := openReadOnlyPool(ctx, url)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	connections, err := forgestore.New(pool).List(ctx)
	if err != nil {
		return nil, err
	}

	stateDir, err := config.GetStateDir()
	if err != nil {
		return nil, err
	}

	credentials := forge.NewFileStore(stateDir)
	statuses := make([]ForgeStatus, 0, len(connections))
	for _, connection := range connections {
		status := ForgeStatus{Host: connection.Host}
		build, ok := forgeBuilders[connection.Kind]
		if !ok {
			status.Err = fmt.Errorf("no forge of kind %q in this build", connection.Kind)
			statuses = append(statuses, status)

			continue
		}

		auth := forge.NewStoredCredential(forge.NewStoredCredentialParams{
			Store:        credentials,
			ConnectionID: connection.ID,
			Header:       forge.HeaderFor(connection.Kind, connection.Method),
			Connections:  forge.NoRecord{},
		})
		status.Login, status.Err = build(connection.APIURL, auth).Verify(ctx)
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// forgeBuilders is every forge this build can speak to, by kind.
var forgeBuilders = map[string]forge.Builder{
	forge.KindGitHub: github.Build,
}
