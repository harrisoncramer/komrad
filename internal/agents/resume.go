package agents

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
)

type harnessRelocator interface {
	Relocate(sessionID, dir string) error
}

func (s *AgentService) getResumeDir(ctx context.Context, agent agentstore.Agent, h harnessRelocator) string {
	dir := config.ExpandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if requireUsableDir(dir) == nil {
		return dir
	}

	if err := os.MkdirAll(dir, 0o755); err == nil && requireUsableDir(dir) == nil {
		return dir
	}

	standIn := filepath.Join(s.fallbackResumeRoot, agent.ID)
	if err := os.MkdirAll(standIn, 0o755); err != nil || requireUsableDir(standIn) != nil {
		home, _ := os.UserHomeDir()

		return home
	}

	if err := h.Relocate(agent.SessionID, standIn); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to carry an agent's conversation to its stand-in directory",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.SessionID, agent.SessionID),
			}, logging.Err(err)...)...)
	}

	return standIn
}
