// Package shells opens a shell on the engine for a person at a terminal somewhere else, rooted
// in a directory some live agent works in.
package shells

import (
	"context"
	"fmt"

	"komrad/internal/agentdirs"
	"komrad/internal/pty"
)

// NewShellServiceParams is what the service is built over.
type NewShellServiceParams struct {
	Agents agentdirs.DirLister
	Shell  string
}

// ShellService opens shells in the directories agents work in.
type ShellService struct {
	agents agentdirs.DirLister
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
	dir, err := agentdirs.Resolve(ctx, s.agents, params.Dir)
	if err != nil {
		return nil, err
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
