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

// interruptedPrompt is what a resumed agent is asked on the way back in, when its previous
// turn was cut off by the pane going. No CLI carries an unfinished turn across a relaunch:
// what comes back is the conversation and an empty prompt, so the only way to have the work
// picked up again is to ask for it as an ordinary message.
const interruptedPrompt = "Your last turn was cut off part-way through when your pane was stopped, " +
	"so nothing you had in flight was carried over. Read back over the conversation, work out what you " +
	"had actually finished and what you had not, and carry on from there. If the work was already done, " +
	"say so rather than doing it again."

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
