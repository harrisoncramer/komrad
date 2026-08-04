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

// harnessRelocator is all a restore needs from the harness it looked up, a chance to
// carry the conversation to wherever the agent is about to come back up.
type harnessRelocator interface {
	Relocate(sessionID, dir string) error
}

// getResumeDir picks the directory a restored agent is relaunched in, repairing
// the recorded one when it has to. A CLI may key a conversation to the directory
// it ran in, so a session can be reachable only from the path the agent was
// created in. That path is frequently a worktree that has since been deleted, and
// a restore that gave up there would strand the one conversation the panel still
// lists, so this always returns somewhere an agent can be started.
func (s *AgentService) getResumeDir(ctx context.Context, agent agentstore.Agent, h harnessRelocator) string {
	dir := config.ExpandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if requireUsableDir(dir) == nil {
		return dir
	}

	// Recreating the missing path is the repair that keeps the conversation, since
	// an empty directory in the same place still names the same directory to a
	// harness that keys on it. It is also inert against the worktree flow that
	// usually removed it: git checks a worktree back out over an empty directory.
	if err := os.MkdirAll(dir, 0o755); err == nil && requireUsableDir(dir) == nil {
		return dir
	}

	// Nothing can be made at that path at all, which a cwd on an unmounted volume
	// or under a directory the user cannot write looks like. Run the agent
	// somewhere that is always ours and let the harness carry the conversation
	// across, so a resume still has it to find.
	standIn := filepath.Join(s.fallbackResumeRoot, agent.ID)
	if err := os.MkdirAll(standIn, 0o755); err != nil || requireUsableDir(standIn) != nil {
		home, _ := os.UserHomeDir()

		return home
	}

	// A failure to carry it is not fatal: losing the conversation is the cost of a resume
	// that finds nothing, while failing here would cost the agent as well. It is logged,
	// since an agent that came back without its history is otherwise unexplained.
	if err := h.Relocate(agent.SessionID, standIn); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to carry an agent's conversation to its stand-in directory",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.SessionID, agent.SessionID),
			}, logging.Err(err)...)...)
	}

	return standIn
}
