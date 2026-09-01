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

	// ActivityUnrecognized is a frame that matched none of a harness's patterns, which is
	// not the same claim as ActivityIdle even though both leave an agent idle today.
	ActivityUnrecognized
)

type Turn struct {
	Role    string
	Content string
}

type LogRead struct {
	Turns    []Turn
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
