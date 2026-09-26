// Package shells opens a shell on the engine for a person at a terminal somewhere else, rooted
// in a directory some live agent works in.
package shells

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/pty"
	agentstore "komrad/internal/store/agent"
)

var (
	ErrOutsideAgents = fail.Denied("the directory is not inside a live agent's directory")
	ErrNoDirectory   = fail.InvalidArgument("the directory does not exist")
)

type agentDirLister interface {
	ListDirs(ctx context.Context) ([]agentstore.AgentDir, error)
}

// NewShellServiceParams is what the service is built over.
type NewShellServiceParams struct {
	Agents agentDirLister
	Shell  string
}

// ShellService opens shells in the directories agents work in.
type ShellService struct {
	agents agentDirLister
	shell  string
}

// NewShellService builds the service.
func NewShellService(params NewShellServiceParams) *ShellService {
	return &ShellService{agents: params.Agents, shell: params.Shell}
}

// OpenParams is the shell a person asked for.
type OpenParams struct {
	Dir  string
	Size pty.Size
	Term string
}

// Open starts a shell in dir, which must be inside a live agent's directory, killed when ctx ends.
func (s *ShellService) Open(ctx context.Context, params OpenParams) (pty.Terminal, error) {
	if !filepath.IsAbs(params.Dir) {
		return nil, fmt.Errorf("%w: %s is not absolute", ErrNoDirectory, params.Dir)
	}

	dir, err := filepath.EvalSymlinks(params.Dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoDirectory, params.Dir)
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNoDirectory, params.Dir)
	}

	isInside, err := s.isInsideLiveAgent(ctx, dir)
	if err != nil {
		return nil, err
	}

	if !isInside {
		return nil, fmt.Errorf("%w: %s", ErrOutsideAgents, params.Dir)
	}

	terminal, err := pty.Start(ctx, pty.StartParams{
		Argv: []string{s.shell},
		Dir:  dir,
		Size: params.Size,
		Term: params.Term,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a shell in %s: %w", dir, err)
	}

	return terminal, nil
}

func (s *ShellService) isInsideLiveAgent(ctx context.Context, dir string) (bool, error) {
	agents, err := s.agents.ListDirs(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to read the directories agents work in: %w", err)
	}

	for _, agent := range agents {
		if agent.ArchivedAt != nil || agent.DeletingAt != nil || agent.Cwd == "" {
			continue
		}

		cwd, err := filepath.EvalSymlinks(agent.Cwd)
		if err != nil {
			continue
		}

		if dir == cwd || strings.HasPrefix(dir, cwd+string(os.PathSeparator)) {
			return true, nil
		}
	}

	return false, nil
}
