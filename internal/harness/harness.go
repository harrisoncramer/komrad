package harness

import (
	"errors"
	"fmt"
	"strings"
)

const AgentIDEnv = "WEAVE_AGENT_ID"

const SessionIDEnv = "WEAVE_SESSION_ID"

// EnvPrefix is the assignments that go ahead of the CLI so an agent runs knowing which agent it
// is and which conversation it is in.
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

type Launch struct {
	Flags string

	Prompt string

	Context string

	SessionID string

	AgentID string

	MCPEndpoint string

	CacheDir string

	Dir string

	HookCommand string

	Resume bool

	// ResumeFrom is a conversation somebody else started, carried into SessionID rather than
	// continued in place, which is what adopting an agent is.
	ResumeFrom string
}

type Activity int

const (
	ActivityUnknown Activity = iota

	ActivityWorking

	ActivityWaiting

	// ActivityIdle is a frame the harness positively recognises as the input box.
	ActivityIdle

	// ActivityUnrecognized is a frame that matched none of a harness's patterns, which is
	// not the same claim as ActivityIdle even though both leave an agent idle today.
	ActivityUnrecognized
)

// ErrSessionNotFound is what a harness answers when no conversation on disk holds an id.
var ErrSessionNotFound = errors.New("no conversation found")

// Session is what a harness can say about a conversation from its log alone, which is where it
// was held and, when the log records one, the branch it was held on.
type Session struct {
	Dir string

	Branch string
}

type Turn struct {
	Role    string
	Content string
}

type LogRead struct {
	Turns []Turn

	Consumed int64

	Restart bool
}

type Harness interface {
	GetID() string

	GetBinary() string

	BuildCommand(Launch) (string, error)

	MintSessionID() string

	FindSession(agentID string) (string, error)

	DescribeSession(sessionID string) (Session, error)

	ForksOnResume() bool

	ReadActivity(frame string) Activity

	GetInterruptKeys() []string

	GetLogPath(sessionID string) string

	ReadLog(path string, offset int64) (LogRead, error)
}
