// Package harness is the seam between the panel and the agent CLI it runs. Everything
// true of one CLI and not another sits behind Harness, so a second one is a new package
// rather than a change to the reconciler, the syncer, or the agent service.
package harness

import (
	"fmt"
	"strings"
)

// AgentIDEnv is the environment variable a launched agent finds the panel's own id for it
// in. It rides on the environment rather than an appended system prompt because it has to
// survive a resume, which an agent that only learned its id at spawn would not.
const AgentIDEnv = "WEAVE_AGENT_ID"

// SessionIDEnv is the environment variable a launched agent finds its own conversation id
// in, for the same reason the agent id does.
const SessionIDEnv = "WEAVE_SESSION_ID"

// EnvPrefix is the assignments that go ahead of the CLI on the command line, so an agent
// runs knowing which agent it is and which conversation it is in. Every harness starts its
// command with this, since what an agent knows about itself is the panel's business rather
// than any one CLI's.
//
// The values are quoted like any other data reaching the pane's shell, and a launch
// missing either leaves that variable unset rather than exported empty.
func EnvPrefix(launch Launch) string {
	var assignments []string
	if launch.AgentID != "" {
		assignments = append(assignments, fmt.Sprintf("%s=%s", AgentIDEnv, QuoteArg(launch.AgentID)))
	}
	if launch.SessionID != "" {
		assignments = append(assignments, fmt.Sprintf("%s=%s", SessionIDEnv, QuoteArg(launch.SessionID)))
	}

	if len(assignments) == 0 {
		return ""
	}

	return strings.Join(assignments, " ") + " "
}

// Launch is everything the panel knows about one start of an agent. How any of it reaches
// the command line belongs to the harness, since the flag that pins a conversation,
// whether a system prompt can be appended, and how an MCP server is registered all differ
// between CLIs.
type Launch struct {
	// Flags are the user's own, already merged from the configured defaults and the
	// agent's. They must reach the CLI as data rather than shell, which QuoteFlags does.
	Flags string
	// Prompt is the opening message, empty on a resume.
	Prompt string
	// Context is the panel's briefing followed by the agent's workflow context, which a
	// harness with somewhere to put it appends to the system prompt. It is handed over on
	// a resume too, for a harness that can re-inject it.
	Context string
	// SessionID names the conversation: the one to reopen on a resume, and on a fresh
	// launch the id it should be filed under, which not every CLI accepts.
	SessionID string
	// AgentID is the panel's own id for this agent, exported under AgentIDEnv.
	AgentID string
	// MCPEndpoint is the host:port serve listens on, where the panel's own MCP server is.
	MCPEndpoint string
	// Dir is the directory the agent runs in. The pane is already opened there, so a
	// harness needs it only for a CLI that has to be told separately, such as one that
	// asks whether the directory is trusted.
	Dir string
	// Resume asks for the prior conversation rather than a new one.
	Resume bool
}

// Activity is what a captured pane says an agent is doing, a verdict about one frame and
// nothing more. How many frames it takes to move a state is the reconciler's.
type Activity int

const (
	// ActivityUnknown is a frame that says nothing either way and leaves the agent's
	// state alone. It is the right answer whenever a harness cannot tell, since a wrong
	// verdict outlives the frame while a missing one costs a pass.
	ActivityUnknown Activity = iota
	// ActivityWorking is an agent generating or running a tool.
	ActivityWorking
	// ActivityWaiting is an agent blocked on the user, at a confirmation or any
	// other interactive prompt.
	ActivityWaiting
	// ActivityIdle is an agent sitting at its input prompt with nothing to do.
	ActivityIdle
)

// Turn is one thing said in a conversation, which is all the panel keeps of it. Tool
// calls, results, and reasoning are not turns, since a search hit has to read as
// something that was actually said.
type Turn struct {
	Role    string
	Content string
}

// LogRead is what one pass over a conversation log found.
type LogRead struct {
	// Turns are the turns the pass read, in the order they were said.
	Turns []Turn
	// Consumed is how many bytes those turns span, which is what the stored offset
	// advances by. A log with no complete new record consumes nothing.
	Consumed int64
	// Restart says the log no longer continues from the offset it was read at, so what
	// is stored describes text that is gone and indexing starts over. A log a resume
	// rewrote looks like this.
	Restart bool
}

// Harness is one agent CLI: everything the panel needs to put it up in a pane, tell what
// it is doing, and read back what was said. An implementation holds all the knowledge of
// that CLI and none of the panel's own policy.
type Harness interface {
	// GetID is the name an agent's harness is recorded under.
	GetID() string
	// BuildCommand renders the command line a pane's shell runs to put the agent up.
	BuildCommand(Launch) (string, error)
	// MintSessionID mints the id a fresh conversation should be filed under, or empty
	// when the CLI will not be told one and it has to be discovered with FindSession.
	MintSessionID() string
	// FindSession returns the conversation this CLI started for the given agent, or empty
	// when it has not written one yet. A CLI that names its own conversations stamps the
	// agent it was told at launch on what it writes, so the answer is that agent's own
	// rather than a candidate to be told apart from another agent's.
	FindSession(agentID string) (string, error)
	// ReadActivity reads a captured frame and reports what it says the agent is doing.
	ReadActivity(frame string) Activity
	// GetLogPath locates the conversation log for a session id, or returns empty when
	// there is none on disk yet.
	GetLogPath(sessionID string) string
	// ReadLog reads what the log at path holds past offset.
	ReadLog(path string, offset int64) (LogRead, error)
	// Relocate carries a conversation's own state to wherever the agent is about to
	// be resumed, for a CLI that keys a conversation to the directory it ran in.
	Relocate(sessionID, dir string) error
}
