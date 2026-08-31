package harnesstest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"weave/internal/harness"

	"github.com/google/uuid"
)

const ID = "fake"

const (
	workingLine = "fake:working"
	waitingLine = "fake:waiting"
	idleLine    = "fake:idle"
)

const tailLines = 5

const agentScript = `#!/bin/sh
log="$FAKE_ROOT/logs/$FAKE_SESSION.jsonl"
state="$FAKE_ROOT/state/$WEAVE_AGENT_ID"
typed="$FAKE_ROOT/typed/$WEAVE_AGENT_ID"
mkdir -p "$FAKE_ROOT/logs" "$FAKE_ROOT/state" "$FAKE_ROOT/typed"

if [ -n "$FAKE_RESUME" ]; then
	printf 'assistant\tresumed\n' >>"$log"
elif [ -n "$FAKE_PROMPT_FILE" ]; then
	printf 'user\t%s\n' "$(cat "$FAKE_PROMPT_FILE")" >>"$log"
fi

(
	while :; do
		printf '%s\n' "$(cat "$state" 2>/dev/null || printf '` + idleLine + `')"
		sleep 0.2
	done
) &

while IFS= read -r line; do
	[ -n "$line" ] || continue
	printf '%s\n' "$line" >>"$typed"
	printf 'user\t%s\n' "$line" >>"$log"
done
`

const scriptShell = "/bin/sh"

const agentArg = "--weave-fake-agent"

func init() {
	if len(os.Args) != 3 || os.Args[1] != agentArg {
		return
	}

	agent := exec.Command(scriptShell, os.Args[2])
	agent.Stdin, agent.Stdout, agent.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := agent.Run(); err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}

type Harness struct {
	root   string
	script string

	binary string
}

var _ harness.Harness = (*Harness)(nil)

// New writes the agent script under root and returns a harness that runs it, keeping everything
// it holds under root.
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

	binary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to find the test binary the fake agents run as: %w", err)
	}

	return &Harness{root: root, script: script, binary: binary}, nil
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

// BuildCommand renders the command line that puts the fake agent up, quoting everything
// interpolated the way a real harness has to.
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

	return command + " " + harness.QuoteArg(h.binary) + " " + agentArg + " " + harness.QuoteArg(h.script), nil
}

// GetInterruptKeys is what the panel would send to stop this harness where it stands.
func (h *Harness) GetInterruptKeys() []string { return []string{"Escape"} }

// ReadActivity reads the bottom of a captured frame, where only the last line the agent
// printed is its current state.
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

// ReadLog reads whatever the log holds past offset, one line per turn of a role and what was
// said.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	return harness.ReadAppendOnlyLog(path, offset, flatten)
}

// SetActivity is what the test writes to move an agent, which shows in its pane within a tick.
func (h *Harness) SetActivity(agentID string, activity harness.Activity) error {
	line := idleLine
	switch activity {
	case harness.ActivityWorking:
		line = workingLine
	case harness.ActivityWaiting:
		line = waitingLine
	case harness.ActivityIdle, harness.ActivityUnknown, harness.ActivityUnrecognized:
	}

	return h.Show(agentID, line)
}

// Show is the line an agent prints on its next tick, which is how a test leaves a pane showing
// something nothing recognises.
func (h *Harness) Show(agentID, line string) error {
	path := filepath.Join(h.root, "state", agentID)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		return fmt.Errorf("failed to set the activity for agent %q: %w", agentID, err)
	}

	return nil
}

// Typed is every line typed at an agent's pane, oldest first, as its own stdin received them
// rather than what tmux was asked to send.
func (h *Harness) Typed(agentID string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(h.root, "typed", agentID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read what was typed at agent %q: %w", agentID, err)
	}

	var typed []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			typed = append(typed, line)
		}
	}

	return typed, nil
}

// Frame is the pane line an agent shows in the given activity, so a test can wait for the
// state it just asked for rather than sleeping.
func Frame(activity harness.Activity) string {
	switch activity {
	case harness.ActivityWorking:
		return workingLine
	case harness.ActivityWaiting:
		return waitingLine
	case harness.ActivityIdle, harness.ActivityUnknown, harness.ActivityUnrecognized:
		return idleLine
	}

	return idleLine
}

// Say appends a turn to a conversation, which is how a test gives the syncer text to index
// without waiting on an agent.
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
