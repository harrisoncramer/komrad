// Package harness is the boundary between weave and the CLI an agent actually runs, so that
// Claude and Codex differ only inside their own packages.
//
// A harness knows what weave cannot know generically: the command line that starts a
// conversation, the keys that interrupt one, where the CLI writes its conversation log and how
// to flatten that log into turns, and how to read a captured frame for whether the agent is
// working or waiting. The registry holds the harnesses this build was compiled with and hands
// one back by the id an agent or workflow named.
package harness

import (
	"fmt"
	"strings"
	"time"
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
	Flags       string
	Prompt      string
	Context     string
	SessionID   string
	AgentID     string
	MCPEndpoint string
	CacheDir    string
	Dir         string
	// HookCommand is the weave binary a harness points its own hooks at, and a harness that
	// wires none, or whose CLI cannot report a turn of its ending, carries no unread mark at
	// all, since that mark is only ever stamped from such a report.
	HookCommand string
	Resume      bool
}

type Activity int

const (
	ActivityUnknown Activity = iota
	ActivityWorking
	ActivityWaiting

	// ActivityIdle is a frame the harness positively recognises as the input box.
	ActivityIdle

	// ActivityComposing is the input box holding text somebody typed and has not sent, so
	// anything weave types there would be fused with it.
	ActivityComposing

	// ActivityUnrecognized is a frame that matched none of a harness's patterns, which is
	// not the same claim as ActivityIdle even though both leave an agent idle today.
	ActivityUnrecognized
)

// String names an activity for a log line or the name of a recorded frame.
func (a Activity) String() string {
	switch a {
	case ActivityWorking:
		return "working"
	case ActivityWaiting:
		return "waiting"
	case ActivityIdle:
		return "idle"
	case ActivityComposing:
		return "composing"
	case ActivityUnrecognized:
		return "unrecognized"
	case ActivityUnknown:
		return "unknown"
	}

	return "unknown"
}

type Turn struct {
	Role    string
	Content string
}

// Usage is one report a CLI wrote about what an exchange with the model cost, keeping the
// counts apart because weave has no way to weigh a cache read against a fresh input token.
type Usage struct {
	// Key identifies the exchange within the agent's own log, so a line the sync loop reads
	// twice is stored once rather than counted twice.
	Key        string
	At         time.Time
	Input      int64
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

type LogRead struct {
	Turns    []Turn
	Usage    []Usage
	Consumed int64
	Restart  bool
}

type Harness interface {
	GetID() string

	GetBinary() string

	BuildCommand(launch Launch) (string, error)

	MintSessionID() string

	FindSession(agentID string) (string, error)

	ReadActivity(frame string) Activity

	GetInterruptKeys() []string

	GetLogPath(sessionID string) string

	ReadLog(path string, offset int64) (LogRead, error)
}
