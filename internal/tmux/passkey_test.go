//go:build integration

package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userRootBindings is a vim-tmux-navigator style config: the key pages the pane in copy-mode
// unless vim is running in it.
const userRootBindings = `
is_vim="ps -o state= -o comm= -t '#{pane_tty}' \
    | grep -iqE '^[^TXZ ]+ +(\\S+\\/)?g?(view|n?vim?x?)(diff)?$'"

bind -n C-d run-shell "if $is_vim; then tmux send-keys C-d; else tmux copy-mode -u; tmux send-keys M; fi"
bind -n C-u run-shell "if $is_vim; then tmux send-keys C-u; else tmux copy-mode -u; tmux send-keys M; fi"
bind -n C-t set -g @first one \; set -g @second two
bind -n C-y display-message unused
`

// TestTakePanelKeysHandsThePressToThePanelPaneOnly pins both arms of the rebinding against a
// real client: the key reaches the program on the panel's pane, and the user's own binding runs
// on any other pane, a multi-command binding included.
func TestTakePanelKeysHandsThePressToThePanelPaneOnly(t *testing.T) {
	client := testServer(t, "passkey")

	config := filepath.Join(t.TempDir(), "tmux.conf")
	require.NoError(t, os.WriteFile(config, []byte(userRootBindings), 0o600))
	_, err := client.query("source-file", config)
	require.NoError(t, err)

	pane, err := client.query("new-window", "-P", "-F", "#{pane_id}", "-t", "passkey:", "--",
		"sh", "-c", "stty raw -echo; cat -v")
	require.NoError(t, err)

	require.NoError(t, client.TakePanelKeys([]string{"ctrl+d", "ctrl+u", "ctrl+t", "ctrl+n", "j"}))

	rebound, err := client.GetRootKeys()
	require.NoError(t, err)

	for _, key := range []string{"C-d", "C-u", "C-t"} {
		assert.True(t, IsPanelPassThrough(rebound[key]), "%s is now the panel's: %s", key, rebound[key])
	}

	assert.Equal(t, "display-message unused", rebound["C-y"], "a key the panel does not bind is left as it was")
	assert.NotContains(t, rebound, "C-n", "a panel key the user never bound is not bound now either")

	require.NoError(t, client.TakePanelKeys([]string{"ctrl+d", "ctrl+u", "ctrl+t"}))

	again, err := client.GetRootKeys()
	require.NoError(t, err)
	assert.Equal(t, rebound, again, "a second panel finds the keys taken and leaves them be")

	attachClient(t, client, "passkey")

	watcher, err := client.query("list-clients", "-t", "passkey", "-F", "#{client_name}")
	require.NoError(t, err)

	press := func(key string) {
		t.Helper()

		_, err := client.query("send-keys", "-K", "-c", watcher, key)
		require.NoError(t, err)
	}

	client.RecordPanelPane(pane)
	press("C-d")
	press("C-u")

	require.Eventually(t, func() bool {
		return strings.Contains(client.CapturePane(pane), "^D^U")
	}, 5*time.Second, 20*time.Millisecond, "the program on the panel's pane reads both presses")
	assert.Equal(t, "0", paneOption(t, client, pane, "pane_in_mode"), "and tmux did not open copy-mode over it")

	client.RecordPanelPane("%999")
	press("C-u")

	require.Eventually(t, func() bool {
		return paneOption(t, client, pane, "pane_in_mode") == "1"
	}, 5*time.Second, 20*time.Millisecond, "on any other pane the user's binding runs and opens copy-mode")
	assert.Equal(t, "copy-mode", paneOption(t, client, pane, "pane_mode"))

	press("q")
	press("C-t")

	require.Eventually(t, func() bool {
		return client.ShowGlobal("@first") == "one" && client.ShowGlobal("@second") == "two"
	}, 5*time.Second, 20*time.Millisecond, "both commands of the user's binding run")
	assert.NotContains(t, client.CapturePane(pane), "^T", "and the pane itself never saw the key")
}

// TestPassKeyToPanelReportsWhatTmuxRefused pins the refusal naming the key, which is what the
// panel logs when a binding stays with tmux.
func TestPassKeyToPanelReportsWhatTmuxRefused(t *testing.T) {
	client := testServer(t, "refused")

	err := client.passKeyToPanel("no such key", "display-message paged")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such key")
}

func paneOption(t *testing.T, c *Client, pane, format string) string {
	t.Helper()

	out, err := c.query("display-message", "-p", "-t", pane, "#{"+format+"}")
	require.NoError(t, err)

	return out
}
