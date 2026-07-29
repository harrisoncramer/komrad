package agents

import (
	"os"
	"path/filepath"
	"strings"

	agentstore "claude-agents/internal/store/agent"
)

// DefaultRestoreRoot is where an agent runs when the directory it was created in
// cannot be brought back. It sits beside Claude's own state rather than in the
// system temp directory, so a stand-in an agent is still living in does not
// vanish under it on the next cleanup sweep.
func DefaultRestoreRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".claude", "claude-agents", "restored")
}

// resumeDir resolves the directory a restored agent is relaunched in, repairing
// the recorded one when it has to. Claude keeps a conversation under a folder
// named after the directory it ran in and resolves --resume against that folder
// alone, so a session is only reachable from the path the agent was created in.
// That path is frequently a worktree that has since been deleted, and a restore
// that gave up there would strand the one conversation the panel still lists, so
// this always returns somewhere an agent can be started.
func (s *AgentService) resumeDir(agent agentstore.Agent) string {
	dir := expandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if usableDir(dir) == nil {
		return dir
	}

	// Recreating the missing path is the repair that keeps the conversation, since
	// an empty directory in the same place still names the same folder under
	// Claude's projects. It is also inert against the worktree flow that usually
	// removed it: git checks a worktree back out over an empty directory.
	if err := os.MkdirAll(dir, 0o755); err == nil && usableDir(dir) == nil {
		return dir
	}

	// Nothing can be made at that path at all, which a cwd on an unmounted volume
	// or under a directory the user cannot write looks like. Run the agent
	// somewhere that is always ours and carry the log across, so --resume still
	// has the conversation to find.
	standIn := filepath.Join(s.restoreRoot, agent.ID)
	if err := os.MkdirAll(standIn, 0o755); err != nil || usableDir(standIn) != nil {
		home, _ := os.UserHomeDir()

		return home
	}

	moveSessionLog(s.transcriptsDir, agent.ClaudeSessionID, standIn)

	return standIn
}

// moveSessionLog moves an agent's conversation log into the projects folder that
// belongs to dir, so a claude started there resumes it rather than opening an
// empty session. It moves rather than copies because the syncer finds a log by
// globbing for the session id, and two files answering to the same id would leave
// it reading whichever sorted first. A failure is swallowed: losing the
// conversation is the cost of a resume that finds nothing, while failing here
// would cost the agent as well.
func moveSessionLog(projectsDir, claudeSessionID, dir string) {
	if projectsDir == "" || claudeSessionID == "" {
		return
	}

	current := sessionLogPath(projectsDir, claudeSessionID)
	if current == "" {
		return
	}

	target := filepath.Join(projectsDir, projectFolder(dir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return
	}

	_ = os.Rename(current, filepath.Join(target, claudeSessionID+".jsonl"))
}

// projectFolder renders the folder name Claude files a directory's conversations
// under, every character outside letters and digits replaced by a dash. This is
// the only place that reproduces Claude's encoding, and it is reached only when a
// log has to be moved between folders. Everything that reads a log globs for the
// session id instead, so an encoding that changes upstream degrades to a resume
// that starts fresh rather than a restore that fails.
func projectFolder(dir string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, dir)
}
