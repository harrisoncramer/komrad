//go:build integration

package tmux

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBindRootKeyTakesTheKeyFromThePanes pins which table a root binding lands in, a key bound
// there firing whatever pane holds the keyboard and stopping that pane ever seeing it.
func TestBindRootKeyTakesTheKeyFromThePanes(t *testing.T) {
	client := testServer(t, "keys")

	client.BindRootKey("F1", "display-message", "root")

	assert.Contains(t, keyTable(t, client, "root"), "F1", "the root table holds it")
	assert.NotContains(t, keyTable(t, client, "prefix"), "F1", "and the prefix table does not")

	client.UnbindRootKey("F1")

	assert.NotContains(t, keyTable(t, client, "root"), "F1", "unbinding gives the key back")
}

// TestBindKeyStaysBehindThePrefix pins that an ordinary binding is only reachable after the
// prefix, which is what keeps it out of the way of whatever is running in the panes.
func TestBindKeyStaysBehindThePrefix(t *testing.T) {
	client := testServer(t, "keys")

	client.BindKey("F2", "display-message", "prefixed")

	assert.Contains(t, keyTable(t, client, "prefix"), "F2")
	assert.NotContains(t, keyTable(t, client, "root"), "F2")

	client.UnbindKey("F2")

	assert.NotContains(t, keyTable(t, client, "prefix"), "F2", "unbinding takes it out of the prefix table")
}

// TestCheckedBindsReportWhatTmuxSaid pins the refusal reaching the caller.
func TestCheckedBindsReportWhatTmuxSaid(t *testing.T) {
	client := testServer(t, "keys")

	require.NoError(t, client.BindKeyChecked("F3", "display-message", "fine"))
	assert.Contains(t, keyTable(t, client, "prefix"), "F3")

	err := client.BindKeyChecked("F4", "no-such-tmux-command")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "F4", "the refusal names the key that was refused")

	require.NoError(t, client.SetGlobalChecked("status", "off"))
	assert.Equal(t, "off", client.ShowGlobal("status"))

	err = client.SetGlobalChecked("no-such-tmux-option", "on")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-tmux-option", "and the refusal names the option")
}

func keyTable(t *testing.T, c *Client, table string) string {
	t.Helper()

	out, err := c.query("list-keys", "-T", table)
	require.NoError(t, err)

	return out
}

// TestFocusEventsAreTurnedOnAtTheServer pins the scope the option is set at, focus being a server
// setting that per session leaves the panel's other windows silent.
func TestFocusEventsAreTurnedOnAtTheServer(t *testing.T) {
	client := testServer(t, "focus")

	client.EnableFocusEvents()

	out, err := client.query("show-options", "-sv", "focus-events")
	require.NoError(t, err)
	assert.Equal(t, "on", strings.TrimSpace(out))
}

// TestInfoPaneRoundTripsAndClears pins the record of where the panel put its info region,
// including the empty value that says there is no longer one.
func TestInfoPaneRoundTripsAndClears(t *testing.T) {
	client := testServer(t, "info")

	assert.Empty(t, client.GetInfoPane(), "nothing is recorded before the panel opens one")

	client.SetInfoPane("%7")
	assert.Equal(t, "%7", client.GetInfoPane())

	client.SetInfoPane("")
	assert.Empty(t, client.GetInfoPane(), "the pane is given up rather than left pointing at a dead one")
}

// TestSocketPathNamesTheServerTheClientIsOn pins the answer weave tells its own server apart
// from the user's by.
func TestSocketPathNamesTheServerTheClientIsOn(t *testing.T) {
	client := testServer(t, "socket")

	path, err := client.GetSocketPath()
	require.NoError(t, err)

	assert.Contains(t, path, "weave-test-", "the socket is the one this client was opened on")
}

// TestSessionOptionIsScopedToItsSession pins that setting one leaves the other sessions alone,
// which is what lets the panel's own session differ from the ones agents run in.
func TestSessionOptionIsScopedToItsSession(t *testing.T) {
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
