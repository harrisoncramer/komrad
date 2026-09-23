//go:build integration

package tmux

import (
	"strings"
	"testing"

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

	client.BindKey("F9", "if-shell", "/usr/local/bin/weave shown --config /etc/weave.toml",
		"run-shell -b 'hide'", "display-popup -T ' weave ' -EE 'open'")

	bound, err := client.GetPrefixKeys()
	require.NoError(t, err)

	for _, tt := range keys {
		assert.Equal(t, "display-message bound", bound[tt.listed], tt.typed)
	}

	assert.Equal(t, `if-shell "/usr/local/bin/weave shown --config /etc/weave.toml" `+
		`"run-shell -b 'hide'" "display-popup -T ' weave ' -EE 'open'"`, bound["F9"],
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

// TestIntegrationSocketPathNamesTheServerTheClientIsOn pins the answer weave tells its own server apart
// from the user's by.
func TestIntegrationSocketPathNamesTheServerTheClientIsOn(t *testing.T) {
	client := testServer(t, "socket")

	path, err := client.GetSocketPath()
	require.NoError(t, err)

	assert.Contains(t, path, "weave-test-", "the socket is the one this client was opened on")
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
