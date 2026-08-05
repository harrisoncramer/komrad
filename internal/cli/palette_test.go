package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/tmux"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindPaneAgentReadsTheStampOffThePane covers how the palette learns which agent it is
// acting on. Nothing is passed to it, since tmux expands no format in a popup's command, so
// the whole answer comes from the TMUX value the popup was started with and the stamp weave
// put on the pane.
func TestFindPaneAgentReadsTheStampOffThePane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	// A socket named rather than given a path, since tmux puts its own somewhere short enough
	// for a unix socket and a temp directory's path is not.
	name := fmt.Sprintf("weave-palette-%d", os.Getpid())
	run := func(args ...string) string {
		t.Helper()

		// The test itself may be running inside tmux, which refuses a nested command.
		cmd := exec.Command("tmux", append([]string{"-L", name}, args...)...)
		cmd.Env = envWithoutTmux()

		out, err := cmd.Output()
		require.NoError(t, err, "tmux %v: %s", args, stderrOf(err))

		return strings.TrimSpace(string(out))
	}

	t.Cleanup(func() { _ = exec.Command("tmux", "-L", name, "kill-server").Run() })

	run("new-session", "-d", "-s", "weave", "-n", "weave", "--", "sh")
	pane := run("new-window", "-d", "-t", "weave:", "-n", "agent", "-P", "-F", "#{pane_id}")
	run("select-window", "-t", "weave:agent")

	// What tmux writes into a pane's TMUX: the socket path, the server's pid, and the id of
	// the session the pane belongs to.
	socket := run("display-message", "-p", "-t", "weave:", "#{socket_path}")
	session := strings.TrimPrefix(run("display-message", "-p", "-t", "weave:", "#{session_id}"), "$")
	inSession := fmt.Sprintf("%s,%d,%s", socket, os.Getpid(), session)

	t.Run("outside tmux there is no pane to act on", func(t *testing.T) {
		_, err := findPaneAgent(&app{env: &config.Environment{}})
		assert.ErrorIs(t, err, errNoInvokingPane)
	})

	t.Run("a pane no agent runs in is refused", func(t *testing.T) {
		_, err := findPaneAgent(&app{env: &config.Environment{Tmux: inSession}})
		assert.ErrorIs(t, err, errNoAgentInPane)
	})

	t.Run("a stamped pane names its agent", func(t *testing.T) {
		require.NoError(t, tmux.NewClientAtPath(socket).SetPaneOwner(pane, "agent-one"))

		found, err := findPaneAgent(&app{env: &config.Environment{Tmux: inSession}})
		require.NoError(t, err)
		assert.Equal(t, "agent-one", found)
	})
}

// stderrOf is what a failed command wrote, which is where tmux says why it refused.
func stderrOf(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ""
	}

	return string(exitErr.Stderr)
}
