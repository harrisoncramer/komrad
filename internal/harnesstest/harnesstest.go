// Package harnesstest is a harness whose agents are a shell script.
//
// Every test above the launcher has had to stub tmux or stub the harness, which stops the
// one chain that matters from being exercised at all: a launch opening a pane, the
// reconciler reading what that pane shows, the syncer reading what was said in it, and a
// resume reopening it. A real CLI cannot be run in a test, so this is a CLI the test
// drives directly. Everything below it stays real.
//
// The agent is a loop printing one line per state into its pane. What that state is comes
// from a file the test writes, so an agent moves between working, waiting, and idle when
// the test says so and never on its own.
package harnesstest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/harness"

	"github.com/google/uuid"
)

// ID is the name an agent running this harness is recorded under.
const ID = "fake"

// The line the agent prints for each state. They are the fake's own chrome rather than any
// real CLI's, so detection here is an exact match instead of the regexes a real TUI needs.
const (
	workingLine = "fake:working"
	waitingLine = "fake:waiting"
	idleLine    = "fake:idle"
)

// tailLines is how many lines from the bottom of a capture a verdict is read from. The
// agent prints one line per tick, so anything above this is a state it has already left.
const tailLines = 5

// agentScript is the agent. It records the opening prompt as a turn, notes a resume so a
// test can tell a reopened conversation from a fresh one, and then prints its current
// state until the pane is killed.
const agentScript = `#!/bin/sh
log="$FAKE_ROOT/logs/$FAKE_SESSION.jsonl"
state="$FAKE_ROOT/state/$WEAVE_AGENT_ID"
mkdir -p "$FAKE_ROOT/logs" "$FAKE_ROOT/state"

if [ -n "$FAKE_RESUME" ]; then
	printf 'assistant\tresumed\n' >>"$log"
elif [ -n "$FAKE_PROMPT_FILE" ]; then
	printf 'user\t%s\n' "$(cat "$FAKE_PROMPT_FILE")" >>"$log"
fi

while :; do
	printf '%s\n' "$(cat "$state" 2>/dev/null || printf '` + idleLine + `')"
	sleep 0.2
done
`

// Harness is the fake CLI. One instance serves every agent in a test, which is why its
// files are keyed by agent and by session rather than held on the struct.
type Harness struct {
	root   string
	script string
}

// The panel reaches an agent CLI only through the interface, so a method missing here is a
// compile error rather than a nil call on one of the service's loops.
var _ harness.Harness = (*Harness)(nil)

// New writes the agent script under root and returns a harness that runs it. Everything
// this harness keeps lives under root, so a test's scratch directory is the whole of it.
func New(root string) (*Harness, error) {
	for _, dir := range []string{"logs", "state"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create the fake harness %s directory: %w", dir, err)
		}
	}

	script := filepath.Join(root, "agent.sh")
	if err := os.WriteFile(script, []byte(agentScript), 0o755); err != nil {
		return nil, fmt.Errorf("failed to write the fake agent script: %w", err)
	}

	return &Harness{root: root, script: script}, nil
}

// GetID is the name an agent running this harness is recorded under.
func (h *Harness) GetID() string { return ID }

// GetBinary is the script this harness runs, which is a path rather than a name on PATH
// since the whole point of the fake is that it is written into a test's own directory.
func (h *Harness) GetBinary() string { return h.script }

// MintSessionID names the conversation before it exists, the way a CLI that takes an id on
// its command line lets the panel do.
func (h *Harness) MintSessionID() string { return uuid.NewString() }

// FindSession has nothing to find, since every conversation is named at launch.
func (h *Harness) FindSession(string) (string, error) { return "", nil }

// BuildCommand renders the command line that puts the fake agent up. It is typed into a
// pane and parsed by the shell running there, so everything interpolated is quoted the way
// a real harness has to quote it.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	command := harness.EnvPrefix(launch) +
		"FAKE_ROOT=" + harness.QuoteArg(h.root) +
		" FAKE_SESSION=" + harness.QuoteArg(launch.SessionID)

	switch {
	case launch.Resume:
		command += " FAKE_RESUME=1"
	case launch.Prompt != "":
		path, err := harness.WriteTempText(h.root, "fake-prompt-*.md", launch.Prompt)
		if err != nil {
			return "", err
		}

		command += " FAKE_PROMPT_FILE=" + harness.QuoteArg(path)
	}

	if quoted := harness.QuoteFlags(launch.Flags); quoted != "" {
		command += " FAKE_FLAGS=" + harness.QuoteArg(quoted)
	}

	return command + " sh " + harness.QuoteArg(h.script), nil
}

// ReadActivity reads the bottom of a captured frame. Only the last line the agent printed
// is its current state; anything above it is one it has already left, and a frame ending
// in something else is a capture landing mid-write rather than a verdict.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}

	for i := len(lines) - 1; i >= 0; i-- {
		switch strings.TrimSpace(lines[i]) {
		case workingLine:
			return harness.ActivityWorking
		case waitingLine:
			return harness.ActivityWaiting
		case idleLine:
			return harness.ActivityIdle
		}
	}

	return harness.ActivityUnknown
}

// GetLogPath is where the conversation for a session id is written, empty until the agent
// has written one.
func (h *Harness) GetLogPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	path := filepath.Join(h.root, "logs", sessionID+".jsonl")
	if _, err := os.Stat(path); err != nil {
		return ""
	}

	return path
}

// ReadLog reads whatever the log holds past offset. Each turn is one line of a role and
// what was said, which is enough for the syncer to have something to index and little
// enough that a test can write one by hand.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	return harness.ReadAppendOnlyLog(path, offset, flatten)
}

// Relocate has nothing to carry. The log is keyed by session id alone, so an agent resumed
// somewhere else finds it where it was.
func (h *Harness) Relocate(string, string) error { return nil }

// SetActivity is what the test writes to move an agent. The agent reads the file every
// tick, so the change shows in its pane within one.
func (h *Harness) SetActivity(agentID string, activity harness.Activity) error {
	line := idleLine
	switch activity {
	case harness.ActivityWorking:
		line = workingLine
	case harness.ActivityWaiting:
		line = waitingLine
	case harness.ActivityIdle, harness.ActivityUnknown:
	}

	path := filepath.Join(h.root, "state", agentID)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		return fmt.Errorf("failed to set the activity for agent %q: %w", agentID, err)
	}

	return nil
}

// Frame is the pane line an agent shows in the given activity, so a test can wait for the
// state it just asked for rather than sleeping.
func Frame(activity harness.Activity) string {
	switch activity {
	case harness.ActivityWorking:
		return workingLine
	case harness.ActivityWaiting:
		return waitingLine
	case harness.ActivityIdle, harness.ActivityUnknown:
		return idleLine
	}

	return idleLine
}

// Say appends a turn to a conversation, standing in for the agent having said something.
// The syncer reads the log rather than the pane, so this is how a test gives it text to
// index without waiting on an agent that only ever prints its state.
func (h *Harness) Say(sessionID, role, content string) error {
	path := filepath.Join(h.root, "logs", sessionID+".jsonl")

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open the log for session %q: %w", sessionID, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := fmt.Fprintf(file, "%s\t%s\n", role, content); err != nil {
		return fmt.Errorf("failed to append a turn for session %q: %w", sessionID, err)
	}

	return nil
}

// flatten turns the log's lines into turns. A line that is not a role and something said
// is skipped, which is what a partial write looks like from here.
func flatten(data []byte) []harness.Turn {
	var turns []harness.Turn
	for raw := range strings.SplitSeq(string(data), "\n") {
		role, content, ok := strings.Cut(raw, "\t")
		if !ok || content == "" {
			continue
		}

		turns = append(turns, harness.Turn{Role: role, Content: harness.CleanText(content)})
	}

	return turns
}
