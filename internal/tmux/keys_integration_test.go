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

// TestIntegrationBindRootKeyTakesTheKeyFromThePanes pins which table a root binding lands in, a key bound
// there firing whatever pane holds the keyboard and stopping that pane ever seeing it.
func TestIntegrationBindRootKeyTakesTheKeyFromThePanes(t *testing.T) {
	client := testServer(t, "keys")

	client.BindRootKey("F1", "display-message", "root")

	assert.Contains(t, keyTable(t, client, "root"), "F1", "the root table holds it")
	assert.NotContains(t, keyTable(t, client, "prefix"), "F1", "and the prefix table does not")

	client.UnbindRootKey("F1")

	assert.NotContains(t, keyTable(t, client, "root"), "F1", "unbinding gives the key back")
}

// TestIntegrationBindKeyStaysBehindThePrefix pins that an ordinary binding is only reachable after the prefix.
func TestIntegrationBindKeyStaysBehindThePrefix(t *testing.T) {
	client := testServer(t, "keys")

	client.BindKey("F2", "display-message", "prefixed")

	assert.Contains(t, keyTable(t, client, "prefix"), "F2")
	assert.NotContains(t, keyTable(t, client, "root"), "F2")

	client.UnbindKey("F2")

	assert.NotContains(t, keyTable(t, client, "prefix"), "F2", "unbinding takes it out of the prefix table")
}

// TestIntegrationGetPrefixKeysReadsBackWhatWasBound pins that every key the listing names is handed back
// as tmux takes it, the ones list-keys escapes or respells included, and pins how a command
// holding a space is printed.
func TestIntegrationGetPrefixKeysReadsBackWhatWasBound(t *testing.T) {
	client := testServer(t, "keys")

	keys := []struct {
		typed  string
		listed string
	}{
		{
			typed:  "F7",
			listed: "F7",
		},
		{
			typed:  "c-space",
			listed: "C-Space",
		},
		{
			typed:  "f8",
			listed: "F8",
		},
		{
			typed:  `"`,
			listed: `"`,
		},
		{
			typed:  "'",
			listed: "'",
		},
		{
			typed:  "#",
			listed: "#",
		},
		{
			typed:  "~",
			listed: "~",
		},
		{
			typed:  `\`,
			listed: `\`,
		},
		{
			typed:  `C-\`,
			listed: `C-\`,
		},
		{
			typed:  `M-\`,
			listed: `M-\`,
		},
		{
			typed:  "M-'",
			listed: "M-'",
		},
		{
			typed:  "M-#",
			listed: "M-#",
		},
	}

	for _, tt := range keys {
		client.BindKey(tt.typed, "display-message", "bound")
	}

	client.BindKey("F9", "if-shell", "/usr/local/bin/komrad shown --config /etc/komrad.toml",
		"run-shell -b 'hide'", "display-popup -T ' komrad ' -EE 'open'")

	bound, err := client.GetPrefixKeys()
	require.NoError(t, err)

	for _, tt := range keys {
		assert.Equal(t, "display-message bound", bound[tt.listed], tt.typed)
	}

	assert.Equal(t, `if-shell "/usr/local/bin/komrad shown --config /etc/komrad.toml" `+
		`"run-shell -b 'hide'" "display-popup -T ' komrad ' -EE 'open'"`, bound["F9"],
		"an argument holding a space is printed in double quotes")

	for listed, command := range bound {
		if command == "display-message bound" {
			client.UnbindKey(listed)
		}
	}

	bound, err = client.GetPrefixKeys()
	require.NoError(t, err)

	for _, tt := range keys {
		assert.NotContains(t, bound, tt.listed, "the key the listing named is the one unbind-key takes")
	}
}

// TestIntegrationCheckedBindsReportWhatTmuxSaid pins the refusal reaching the caller.
func TestIntegrationCheckedBindsReportWhatTmuxSaid(t *testing.T) {
	client := testServer(t, "keys")

	require.NoError(t, client.BindKeyChecked("F3", "display-message", "fine"))
	assert.Contains(t, keyTable(t, client, "prefix"), "F3")

	err := client.BindKeyChecked("F4", "no-such-tmux-command")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "F4", "the refusal names the key that was refused")
}

func keyTable(t *testing.T, c *Client, table string) string {
	t.Helper()

	out, err := c.query("list-keys", "-T", table)
	require.NoError(t, err)

	return out
}

// TestIntegrationFocusEventsAreTurnedOnAtTheServer pins the scope the option is set at, focus being a server
// setting that per session leaves the panel's other windows silent.
func TestIntegrationFocusEventsAreTurnedOnAtTheServer(t *testing.T) {
	client := testServer(t, "focus")

	client.EnableFocusEvents()

	out, err := client.query("show-options", "-sv", "focus-events")
	require.NoError(t, err)
	assert.Equal(t, "on", strings.TrimSpace(out))
}

// TestIntegrationInfoPaneRoundTripsAndClears pins the record of where the panel put its info region,
// including the empty value that says there is no longer one.
func TestIntegrationInfoPaneRoundTripsAndClears(t *testing.T) {
	client := testServer(t, "info")

	assert.Empty(t, client.GetInfoPane(), "nothing is recorded before the panel opens one")

	client.SetInfoPane("%7")
	assert.Equal(t, "%7", client.GetInfoPane())

	client.SetInfoPane("")
	assert.Empty(t, client.GetInfoPane(), "the pane is given up rather than left pointing at a dead one")
}

// TestIntegrationSocketPathNamesTheServerTheClientIsOn pins the answer komrad tells its own server apart
// from the user's by.
func TestIntegrationSocketPathNamesTheServerTheClientIsOn(t *testing.T) {
	client := testServer(t, "socket")

	path, err := client.GetSocketPath()
	require.NoError(t, err)

	assert.Contains(t, path, "komrad-test-", "the socket is the one this client was opened on")
}

// TestIntegrationSessionOptionIsScopedToItsSession pins that setting one leaves the other sessions alone,
// which is what lets the panel's own session differ from the ones agents run in.
func TestIntegrationSessionOptionIsScopedToItsSession(t *testing.T) {
	client := testServer(t, "scoped")

	_, err := client.query("new-session", "-d", "-s", "other", "--", "sh")
	require.NoError(t, err)

	client.SetSessionOption("scoped", "status", "off")

	assert.Equal(t, "off", sessionOption(t, client, "scoped", "status"))
	assert.Empty(t, sessionOption(t, client, "other", "status"),
		"the other session is left to inherit rather than written to")
	assert.Equal(t, "on", client.ShowGlobal("status"), "and the global it inherits is untouched")
}

func sessionOption(t *testing.T, c *Client, session, name string) string {
	t.Helper()

	out, err := c.query("show-options", "-t", session+":", "-qv", name)
	require.NoError(t, err)

	return strings.TrimSpace(out)
}

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
