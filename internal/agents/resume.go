package agents

import (
	"os"
	"path/filepath"

	agentstore "weave/internal/store/agent"
)

// relocateHarness is all a restore needs from the harness it resolved, a chance to
// carry the conversation to wherever the agent is about to come back up.
type relocateHarness interface {
	Relocate(sessionID, dir string) error
}

// resumeDir resolves the directory a restored agent is relaunched in, repairing
// the recorded one when it has to. A CLI may key a conversation to the directory
// it ran in, so a session can be reachable only from the path the agent was
// created in. That path is frequently a worktree that has since been deleted, and
// a restore that gave up there would strand the one conversation the panel still
// lists, so this always returns somewhere an agent can be started.
func (s *AgentService) resumeDir(agent agentstore.Agent, h relocateHarness) string {
	dir := expandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if usableDir(dir) == nil {
		return dir
	}

	// Recreating the missing path is the repair that keeps the conversation, since
	// an empty directory in the same place still names the same directory to a
	// harness that keys on it. It is also inert against the worktree flow that
	// usually removed it: git checks a worktree back out over an empty directory.
	if err := os.MkdirAll(dir, 0o755); err == nil && usableDir(dir) == nil {
		return dir
	}

	// Nothing can be made at that path at all, which a cwd on an unmounted volume
	// or under a directory the user cannot write looks like. Run the agent
	// somewhere that is always ours and let the harness carry the conversation
	// across, so a resume still has it to find.
	standIn := filepath.Join(s.restoreRoot, agent.ID)
	if err := os.MkdirAll(standIn, 0o755); err != nil || usableDir(standIn) != nil {
		home, _ := os.UserHomeDir()

		return home
	}

	// A failure to carry it is swallowed: losing the conversation is the cost of a
	// resume that finds nothing, while failing here would cost the agent as well.
	_ = h.Relocate(agent.SessionID, standIn)

	return standIn
}
