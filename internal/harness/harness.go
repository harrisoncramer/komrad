// Package harness is the boundary between komrad and the CLI an agent actually runs.
//
// A harness knows what komrad cannot know generically: the command line that starts a
// conversation, the keys that interrupt one, where the CLI writes its conversation log and how
// to flatten that log into turns, how to read a captured frame for whether the agent is working
// or waiting, and how to read one of its own hook payloads. The registry holds the harnesses
// this build was compiled with and hands one back by the id an agent or workflow named.
package harness

import (
	"fmt"
	"strings"
)

const AgentIDEnv = "KOMRAD_AGENT_ID"

const SessionIDEnv = "KOMRAD_SESSION_ID"

// EnvPrefix is the assignments that go ahead of the CLI so an agent runs knowing which agent it
// is, which conversation it is in, and where komrad's stand-ins for the tools it calls are.
func EnvPrefix(launch Launch) string {
	var assignments []string
	if launch.ToolsDir != "" {
		assignments = append(assignments, "PATH="+QuoteArg(launch.ToolsDir)+`:"$PATH"`)
	}
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
	// HookCommand is the komrad binary a harness points its own hooks at, a harness that wires none
	// leaving the turn end to the reconciler.
	HookCommand string
	// ToolsDir holds komrad's stand-ins for the tools an agent calls, and goes ahead of the PATH
	// the agent runs with.
	ToolsDir       string
	Resume         bool
	Role           string
	AutoCompaction int
}

type Activity int

const (
	ActivityUnknown Activity = iota
	ActivityWorking
	ActivityWaiting

	// ActivityIdle is a frame the harness positively recognises as the input box.
	ActivityIdle

	// ActivityComposing is the input box holding text somebody typed and has not sent, which
	// anything komrad typed there would be fused with.
	ActivityComposing

	// ActivityUnrecognized is a frame that matched none of a harness's patterns.
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

// Input is what a harness read out of one captured frame: what the frame shows the agent doing,
// and the text its input box holds.
type Input struct {
	Activity Activity

	// Held is the text the input box holds, one entry per line the box draws it on, and empty for
	// a frame showing no box.
	Held []string
}

// Text is the whole of what the input box holds.
func (i Input) Text() string {
	return strings.Join(i.Held, "\n")
}

// IsOneLine reports whether every line the input box holds but one is blank.
func (i Input) IsOneLine() bool {
	filled := 0
	for _, line := range i.Held {
		if strings.TrimSpace(line) != "" {
			filled++
		}
	}

	return filled == 1
}

// Event is one hook event a harness answers, spelled the way the CLI firing it spells the event.
type Event string

// Report is what a harness read out of one of its own hook payloads, which annotates the state
// the pane decided rather than deciding one of its own.
type Report struct {
	// Reason is what to say the agent is doing, an empty one clearing whatever was reported
	// before it.
	Reason string

	// TurnEnded says the payload reported a turn of the agent's ending.
	TurnEnded bool
}

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
	ID() string

	Binary() string

	BuildCommand(launch Launch) (string, error)

	TrustDir(dir string) error

	MintSessionID() string

	FindSession(agentID string) (string, error)

	ReadInput(frame string) Input

	ReadReport(event Event, payload []byte) (Report, error)

	InterruptKeys() []string

	LogPath(sessionID string) string

	ReadLog(path string, offset int64) (LogRead, error)
}
