// Package harnesstest is a harness for tests: a script standing in for a CLI, which can be told
// to look like it is working, waiting on a person, or idle.
package harnesstest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"komrad/internal/harness"

	"github.com/stretchr/testify/require"
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
state="$FAKE_ROOT/state/$KOMRAD_AGENT_ID"
received="$FAKE_ROOT/received/$KOMRAD_AGENT_ID"
mkdir -p "$FAKE_ROOT/logs" "$FAKE_ROOT/state" "$FAKE_ROOT/received"

if [ -n "$FAKE_RESUME" ]; then
	printf 'assistant\tresumed\n' >>"$log"
elif [ -n "$FAKE_PROMPT_FILE" ]; then
	printf 'user\t%s\n' "$(cat "$FAKE_PROMPT_FILE")" >>"$log"
fi

(
	while [ -d "$FAKE_ROOT" ]; do
		printf '%s\n' "$(cat "$state" 2>/dev/null || printf '` + idleLine + `')" || exit 0
		sleep 0.2
	done
) &

while IFS= read -r line; do
	[ -n "$line" ] || continue
	printf '%s\n' "$line" >>"$received"
	printf 'user\t%s\n' "$line" >>"$log"
done
`

const scriptShell = "/bin/sh"

const agentArg = "--komrad-fake-agent"

func init() {
	if len(os.Args) != 4 || os.Args[1] != agentArg {
		return
	}

	if err := runAgent(os.Args[2], os.Args[3]); err != nil {
		os.Exit(1) //nolint:forbidigo // the test binary is standing in for an agent process here
	}

	os.Exit(0) //nolint:forbidigo // the test binary is standing in for an agent process here
}

// runAgent runs the agent script, feeding it every line typed at the pane or written to its
// socket.
func runAgent(script, socket string) error {
	agent := exec.Command(scriptShell, script)
	agent.Stdout, agent.Stderr = os.Stdout, os.Stderr

	stdin, err := agent.StdinPipe()
	if err != nil {
		return err
	}

	var mu sync.Mutex
	forward := func(from io.Reader) {
		scanner := bufio.NewScanner(from)
		for scanner.Scan() {
			mu.Lock()
			_, _ = fmt.Fprintln(stdin, scanner.Text())
			mu.Unlock()
		}
	}

	// A relaunched agent finds the socket its last process left.
	_ = os.Remove(socket)

	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = conn.Close() }()
				forward(conn)
			}()
		}
	}()

	go forward(os.Stdin)

	return agent.Run()
}

type Harness struct {
	root    string
	sockets string
	script  string
	binary  string
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

	// A socket path under the root would pass the length a unix socket can have.
	sockets, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to create the fake harness socket directory: %w", err)
	}

	return &Harness{
		root:    root,
		sockets: sockets,
		script:  script,
		binary:  binary,
	}, nil
}

// Close removes the directory the fake agents' sockets are in.
func (h *Harness) Close() error {
	return os.RemoveAll(h.sockets)
}

// ID is the name an agent running this harness is recorded under.
func (h *Harness) ID() string {
	return ID
}

// Binary is the script this harness runs, as a path rather than a name on PATH.
func (h *Harness) Binary() string {
	return h.script
}

// SessionNaming is SessionMinted.
func (h *Harness) SessionNaming() harness.SessionNaming {
	return harness.SessionMinted
}

// TrustDir has nothing to record, the fake agent asking no question at startup.
func (h *Harness) TrustDir(string) error {
	return nil
}

// BuildCommand renders the command line that puts the fake agent up, quoting everything
// interpolated the way a real harness has to.
func (h *Harness) BuildCommand(launch harness.Launch) (string, error) {
	socket, err := harness.PrepareSocket(h.sockets, ID, launch.AgentID)
	if err != nil {
		return "", err
	}

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

	return command + " " + harness.QuoteArg(h.binary) + " " + agentArg + " " + harness.QuoteArg(h.script) + " " + harness.QuoteArg(socket), nil
}

// ReadInput reads the bottom of a captured frame, where only the last line the agent
// printed is its current state.
func (h *Harness) ReadInput(frame string) harness.Input {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}

	for i := len(lines) - 1; i >= 0; i-- {
		switch strings.TrimSpace(lines[i]) {
		case workingLine:
			return harness.Input{Activity: harness.ActivityWorking}
		case waitingLine:
			return harness.Input{Activity: harness.ActivityWaiting}
		case idleLine:
			return harness.Input{Activity: harness.ActivityIdle, Held: []string{""}}
		}
	}

	return harness.Input{Activity: harness.ActivityUnknown}
}

// LogPath is where the conversation for a session id is written, empty until the agent
// has written one.
func (h *Harness) LogPath(sessionID string) string {
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
	case harness.ActivityIdle, harness.ActivityUnknown, harness.ActivityComposing, harness.ActivityUnrecognized:
	}

	return h.Show(agentID, line)
}

// Show is the line an agent prints on its next tick.
func (h *Harness) Show(agentID, line string) error {
	path := filepath.Join(h.root, "state", agentID)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		return fmt.Errorf("failed to set the activity for agent %q: %w", agentID, err)
	}

	return nil
}

// Received is every line that reached an agent, oldest first, as its own stdin received them from
// its socket or its pane.
func (h *Harness) Received(agentID string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(h.root, "received", agentID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read what reached agent %q: %w", agentID, err)
	}

	var received []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			received = append(received, line)
		}
	}

	return received, nil
}

// Say appends a turn to a conversation.
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

// Channel writes a line to the socket the fake agent listens on.
func (h *Harness) Channel() harness.Channel {
	return socketChannel{sockets: h.sockets}
}

// MinVersion is empty, the fake agent having no version to check.
func (h *Harness) MinVersion() string {
	return ""
}

type socketChannel struct {
	sockets string
}

// Send writes text to the agent's socket, returning false when nothing listens there.
func (c socketChannel) Send(ctx context.Context, target harness.Target, text string) (bool, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", harness.SocketPath(c.sockets, ID, target.AgentID))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to reach the fake agent %q: %w", target.AgentID, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := fmt.Fprintln(conn, text); err != nil {
		return false, fmt.Errorf("failed to write to the fake agent %q: %w", target.AgentID, err)
	}

	return true, nil
}

// Hooks is none, the test harness running no hooks of its own.
func (h *Harness) Hooks() []string {
	return nil
}

// ReadReport reads nothing, the test harness running no hooks of its own.
func (h *Harness) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}

// SocketDir is a directory short enough to hold an agent's socket, removed when the test ends.
func SocketDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "") //nolint:usetesting // a path under t.TempDir passes the length a unix socket can have
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}
