//go:build integration

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var created atomic.Int64

func newInstance(t *testing.T) (*instance, string) {
	t.Helper()

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux is not installed: %v", err)
	}

	name := fmt.Sprintf("komradrec-%d-%d", os.Getpid(), created.Add(1))
	inner := tmuxServer{socket: name + "-in", args: []string{"-f", "/dev/null"}}
	outer := tmuxServer{socket: name + "-out", args: []string{"-f", "/dev/null"}}

	ctx := t.Context()

	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", inner.socket, "kill-server").Run()
		_ = exec.Command("tmux", "-L", outer.socket, "kill-server").Run()
	})

	pane, err := inner.run(ctx, "new-session", "-d", "-s", "komrad", "-x", "80", "-y", "24", "-P", "-F", "#{pane_id}", "cat")
	require.NoError(t, err)

	_, err = inner.run(ctx, "set-option", "-g", "@komrad_panel_pane", pane)
	require.NoError(t, err)

	_, err = inner.run(ctx, "set-option", "-g", "@komrad_panel_build", "test")
	require.NoError(t, err)

	recorded := &instance{
		inner:   inner,
		outer:   outer,
		session: "komrad",
		cols:    80,
		rows:    24,
	}

	_, err = recorded.requirePanel(ctx)
	require.NoError(t, err)

	t.Cleanup(recorded.close)

	return recorded, pane
}

func waitForPane(t *testing.T, recorded *instance, pane, want string) {
	t.Helper()

	ctx := t.Context()
	var last string

	for range 100 {
		frame, err := recorded.inner.run(ctx, "capture-pane", "-p", "-t", pane)
		require.NoError(t, err)

		if strings.Contains(frame, want) {
			return
		}

		last = frame

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("pane %s never showed %q; last frame:\n%s", pane, want, last)
}

// TestIntegrationSendKeysReachesThePane types into the panel and sees the text arrive in its pane.
func TestIntegrationSendKeysReachesThePane(t *testing.T) {
	t.Parallel()

	recorded, pane := newInstance(t)
	ctx := t.Context()

	require.NoError(t, recorded.typeText(ctx, "hello"))
	require.NoError(t, recorded.sendKeys(ctx, "Enter"))
	waitForPane(t, recorded, pane, "hello\n")
}

// TestIntegrationSendKeysFiresARootBinding presses a key the root table binds and sees the binding run.
func TestIntegrationSendKeysFiresARootBinding(t *testing.T) {
	t.Parallel()

	recorded, pane := newInstance(t)
	ctx := t.Context()

	_, err := recorded.inner.run(ctx, "bind-key", "-n", "F5", "send-keys", "-t", pane, "-l", "BOUND")
	require.NoError(t, err)

	require.NoError(t, recorded.sendKeys(ctx, "F5"))
	waitForPane(t, recorded, pane, "BOUND")
}
