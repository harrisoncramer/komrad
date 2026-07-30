// Package harness is the seam between the panel and the agent CLI it runs. The
// panel knows how to open a pane, watch what happens in it, and index what was
// said; everything that is true of one CLI and not another sits behind Harness,
// so a second one is a new package rather than a change to the reconciler, the
// syncer, or the agent service.
package harness

// AgentIDEnv is the environment variable a launched agent finds the panel's own
// id for it in. It is the panel's name for itself rather than any one CLI's, so
// it is declared here and read by every harness. It rides on the environment
// rather than in an appended system prompt because it has to survive a resume,
// and an agent that only learned its id at spawn would go anonymous the first
// time it was restored.
const AgentIDEnv = "CLAUDE_AGENTS_AGENT_ID"

// Launch is everything the panel knows about one start of an agent. How any of it
// reaches the command line belongs to the harness: the flag that pins a
// conversation, whether a system prompt can be appended at all, and how an MCP
// server is registered for a single invocation all differ between CLIs.
type Launch struct {
	// Flags are the user's own flags, already merged from the configured defaults
	// and the agent's own. They arrive as one string the way a person types them
	// and must reach the CLI as data rather than as shell, which is what QuoteFlags
	// is for.
	Flags string
	// Prompt is the opening message, empty on a resume.
	Prompt string
	// Context is the panel's briefing followed by the agent's workflow context. A
	// harness with somewhere to put it appends it to the system prompt. It is
	// passed on a resume too, since a harness that can re-inject it should, even
	// though the one that appends a system prompt cannot.
	Context string
	// SessionID names the conversation. On a resume it is the conversation to
	// reopen; on a fresh launch it is the id the conversation should be filed
	// under, which not every CLI accepts.
	SessionID string
	// AgentID is the panel's own id for this agent, which the harness exports into
	// the CLI's environment under AgentIDEnv.
	AgentID string
	// MCPEndpoint is the host:port serve listens on, which is where the panel's own
	// read-only MCP server lives.
	MCPEndpoint string
	// Resume asks for the prior conversation rather than a new one.
	Resume bool
}

// Activity is what a captured pane says an agent is doing. It is a verdict about
// one frame and nothing more: how many consecutive frames it takes to move an
// agent's state is the reconciler's, since that is true of every CLI.
type Activity int

const (
	// ActivityUnknown is a frame that says nothing either way, which leaves the
	// state the agent already holds alone. It is the right answer whenever a
	// harness cannot tell, since a wrong verdict outlives the frame it was read
	// from and a missing one costs a single pass.
	ActivityUnknown Activity = iota
	// ActivityWorking is an agent generating or running a tool.
	ActivityWorking
	// ActivityWaiting is an agent blocked on the user, at a confirmation or any
	// other interactive prompt.
	ActivityWaiting
	// ActivityIdle is an agent sitting at its input prompt with nothing to do.
	ActivityIdle
)

// Turn is one thing said in a conversation, which is all the panel keeps of it.
// Tool calls, their results, and reasoning are not turns: a search hit has to
// read as something that was actually said.
type Turn struct {
	Role    string
	Content string
}

// LogRead is what one pass over a conversation log found.
type LogRead struct {
	// Turns are the turns the pass read, in the order they were said.
	Turns []Turn
	// Consumed is how many bytes the records read span, which is what the stored
	// offset advances by. A pass that consumed nothing changes nothing, which is
	// how a log with no complete new record reports itself.
	Consumed int64
	// Restart says the log no longer continues from the offset it was read at, so
	// what is already stored describes text that is no longer there and indexing
	// starts over. A log a resume rewrote looks like this.
	Restart bool
}

// Harness is one agent CLI: everything the panel needs to put it up in a pane,
// tell what it is doing, and read back what was said in it. An implementation
// holds all the knowledge of that CLI and none of the panel's own policy.
type Harness interface {
	// ID is the name an agent's harness is recorded under.
	ID() string
	// Command renders the command line a pane's shell runs to put the agent up.
	Command(Launch) (string, error)
	// NewSessionID mints the id a fresh conversation should be filed under, or
	// returns empty when the CLI will not be told one and the id has to be
	// discovered after the fact.
	NewSessionID() string
	// Activity reads a captured frame and reports what it says the agent is doing.
	Activity(frame string) Activity
	// LogPath locates the conversation log for a session id, or returns empty when
	// there is none on disk yet.
	LogPath(sessionID string) string
	// ReadLog reads what the log at path holds past offset.
	ReadLog(path string, offset int64) (LogRead, error)
	// Relocate carries a conversation's own state to wherever the agent is about to
	// be resumed, for a CLI that keys a conversation to the directory it ran in.
	Relocate(sessionID, dir string) error
}
