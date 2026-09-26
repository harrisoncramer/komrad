// Package agentdirs decides whether a directory on the engine is one a live agent works in.
package agentdirs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/fail"
	agentstore "komrad/internal/store/agent"
)

var (
	ErrOutsideAgents = fail.Denied("the directory is not inside a live agent's directory")
	ErrNoDirectory   = fail.InvalidArgument("the directory does not exist")
)

// DirLister reads the directories agents work in.
type DirLister interface {
	ListDirs(ctx context.Context) ([]agentstore.AgentDir, error)
}

// Resolve is dir with its links resolved, refused unless it is a directory inside a live agent's
// directory.
func Resolve(ctx context.Context, agents DirLister, dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: %s is not absolute", ErrNoDirectory, dir)
	}

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNoDirectory, dir)
	}

	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrNoDirectory, dir)
	}

	isInside, err := isInsideLiveAgent(ctx, agents, resolved)
	if err != nil {
		return "", err
	}

	if !isInside {
		return "", fmt.Errorf("%w: %s", ErrOutsideAgents, dir)
	}

	return resolved, nil
}

func isInsideLiveAgent(ctx context.Context, agents DirLister, dir string) (bool, error) {
	dirs, err := agents.ListDirs(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to read the directories agents work in: %w", err)
	}

	for _, agent := range dirs {
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
