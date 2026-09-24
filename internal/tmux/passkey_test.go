//go:build integration

package tmux

import (
	"os"
	"path/filepath"
	"strconv"
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
bind -n \; display-message semicolon
bind -n "'" display-message quote
`

// TestIntegrationTakePanelKeysHandsThePressToThePanelPaneOnly pins both arms of the rebinding against a
// real client: the key reaches the program on the panel's pane, and the user's own binding runs
// on any other pane, a multi-command binding included.
func TestIntegrationTakePanelKeysHandsThePressToThePanelPaneOnly(t *testing.T) {
	client := testServer(t, "passkey")

	config := filepath.Join(t.TempDir(), "tmux.conf")
	require.NoError(t, os.WriteFile(config, []byte(userRootBindings), 0o600))
	_, err := client.query("source-file", config)
	require.NoError(t, err)

	pane, err := client.query("new-window", "-P", "-F", "#{pane_id}", "-t", "passkey:", "--",
		"sh", "-c", `printf '\033[?1000h\033[?1006h'; stty raw -echo; cat -v`)
	require.NoError(t, err)

	require.NoError(t, client.TakePanelKeys([]string{"ctrl+d", "ctrl+u", "ctrl+t", "ctrl+n", "j", ";", "'"}))

	rebound, err := client.GetRootKeys()
	require.NoError(t, err)

	for _, key := range []string{"C-d", "C-u", "C-t", ";", "'"} {
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
	press(`\;`)
	press("'")

	require.Eventually(t, func() bool {
		return strings.Contains(client.CapturePane(pane), "^D^U;'")
	}, 5*time.Second, 20*time.Millisecond, "the program on the panel's pane reads every press")
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

// TestIntegrationPassKeyToPanelReportsWhatTmuxRefused pins the refusal naming the key.
func TestIntegrationPassKeyToPanelReportsWhatTmuxRefused(t *testing.T) {
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

// TestIntegrationTakePanelWheelHandsThePanelPaneTheMouse pins the wheel binding's shape: the pane
// under the mouse is handed the mouse event when it is the panel's, and the user's own command
// runs everywhere else.
func TestIntegrationTakePanelWheelHandsThePanelPaneTheMouse(t *testing.T) {
	client := testServer(t, "wheel")

	config := filepath.Join(t.TempDir(), "tmux.conf")
	require.NoError(t, os.WriteFile(config, []byte(`
bind -n WheelUpPane if-shell -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { copy-mode -e }
`), 0o600))
	_, err := client.query("source-file", config)
	require.NoError(t, err)

	require.NoError(t, client.TakePanelWheel())

	rebound, err := client.GetRootKeys()
	require.NoError(t, err)

	for _, wheel := range []string{"WheelUpPane", "WheelDownPane"} {
		assert.True(t, IsPanelPassThrough(rebound[wheel]), "%s is now the panel's: %s", wheel, rebound[wheel])
		assert.Contains(t, rebound[wheel], "-t = ", "the pane under the mouse is the one asked and handed the event")
		assert.Contains(t, rebound[wheel], "pane_in_mode", "copy-mode over the panel keeps the wheel")
		assert.Contains(t, rebound[wheel], panelWheel)
	}

	assert.Contains(t, rebound["WheelUpPane"], "copy-mode -e", "the user's command is the other arm")
	assert.Contains(t, rebound["WheelDownPane"], `"send-keys -M"`, "a wheel tmux never bound is handed to the pane elsewhere")

	require.NoError(t, client.TakePanelWheel())

	again, err := client.GetRootKeys()
	require.NoError(t, err)
	assert.Equal(t, rebound, again, "a second panel finds the wheel taken and leaves it be")

	pane, err := client.query("new-window", "-P", "-F", "#{pane_id}", "-t", "wheel:", "--", "cat")
	require.NoError(t, err)

	other, err := client.query("split-window", "-P", "-F", "#{pane_id}", "-t", pane, "--", "cat")
	require.NoError(t, err)

	client.RecordPanelPane(pane)
	assert.Equal(t, "1", paneOption(t, client, pane, wheelPaneFormat[2:len(wheelPaneFormat)-1]), "the panel's pane takes the wheel")
	assert.Equal(t, "0", paneOption(t, client, other, wheelPaneFormat[2:len(wheelPaneFormat)-1]), "any other pane keeps the user's binding")

	_, err = client.query("copy-mode", "-t", pane)
	require.NoError(t, err)
	assert.Equal(t, "0", paneOption(t, client, pane, wheelPaneFormat[2:len(wheelPaneFormat)-1]), "copy-mode over the panel keeps the user's binding")
	_, err = client.query("send-keys", "-t", pane, "-X", "cancel")
	require.NoError(t, err)
}

// TestIntegrationWheelOverThePanelReachesIt turns a real wheel, written to the client's terminal as
// the mouse sequence, over each pane of a split window.
func TestIntegrationWheelOverThePanelReachesIt(t *testing.T) {
	client := testServer(t, "turn")
	client.SetGlobal("mouse", "on")

	config := filepath.Join(t.TempDir(), "tmux.conf")
	require.NoError(t, os.WriteFile(config, []byte(`
bind -n WheelUpPane if-shell -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { set -g @turned yes }
`), 0o600))
	_, err := client.query("source-file", config)
	require.NoError(t, err)

	pane, err := client.query("new-window", "-P", "-F", "#{pane_id}", "-t", "turn:", "--",
		"sh", "-c", `printf '\033[?1000h\033[?1006h'; stty raw -echo; cat -v`)
	require.NoError(t, err)

	other, err := client.query("split-window", "-v", "-P", "-F", "#{pane_id}", "-t", pane, "--", "cat")
	require.NoError(t, err)

	require.NoError(t, client.TakePanelWheel())
	client.RecordPanelPane(pane)

	outer := attachClient(t, client, "turn")

	wheelUp := func(row int) {
		t.Helper()

		_, err := outer.query("send-keys", "-l", "\x1b[<64;5;"+strconv.Itoa(row)+"M")
		require.NoError(t, err)
	}

	wheelUp(2)

	require.Eventually(t, func() bool {
		return strings.Contains(client.CapturePane(pane), "[<64;")
	}, 5*time.Second, 20*time.Millisecond, "the wheel over the panel reaches it as a mouse event")
	assert.Empty(t, client.ShowGlobal("@turned"), "and the user's binding did not run")

	wheelUp(20)

	require.Eventually(t, func() bool {
		return client.ShowGlobal("@turned") == "yes"
	}, 5*time.Second, 20*time.Millisecond, "the wheel over any other pane runs the user's binding")
	assert.NotContains(t, client.CapturePane(other), "[<64;")
}
