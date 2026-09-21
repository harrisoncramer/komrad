//go:build integration

package tmux

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsAnyClientRecent covers the times tmux writes for a session's clients, which are whole
// seconds since the epoch.
func TestIsAnyClientRecent(t *testing.T) {
	bound := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	at := func(when time.Time) string { return strconv.FormatInt(when.Unix(), 10) }

	tests := []struct {
		name   string
		listed string
		want   bool
	}{
		{name: "a session nobody is attached to"},
		{
			name:   "a client that has taken no key since the bound",
			listed: at(bound.Add(-time.Second)),
		},
		{
			name:   "one of two clients that took a key after it",
			listed: at(bound.Add(-time.Hour)) + "\n" + at(bound.Add(time.Second)),
			want:   true,
		},
		{
			name:   "a client whose last key landed on the bound itself",
			listed: at(bound),
			want:   true,
		},
		{
			name:   "a client whose time tmux did not write as a number",
			listed: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isAnyClientRecent(tt.listed, bound))
		})
	}
}

// TestIsClientAtPaneWithNobodyAttached covers a pane whose session no client is watching, which
// is every agent while the panel is closed.
func TestIsClientAtPaneWithNobodyAttached(t *testing.T) {
	client := testServer(t, "alone")

	pane, err := client.query("display-message", "-p", "-t", "alone:", "#{pane_id}")
	require.NoError(t, err)

	assert.False(t, client.IsClientAtPane(pane, time.Minute))
}

// TestIsClientAtPaneWithSomebodyWatching covers a client attached to the pane's session, and the
// same client once it has looked away at another window.
func TestIsClientAtPaneWithSomebodyWatching(t *testing.T) {
	client := testServer(t, "watched")

	pane, err := client.query("display-message", "-p", "-t", "watched:", "#{pane_id}")
	require.NoError(t, err)

	attachClient(t, client, "watched")

	assert.True(t, client.IsClientAtPane(pane, time.Minute),
		"the attach itself is a keystroke, so the client counts as being there")

	_, err = client.query("new-window", "-d", "-t", "watched:", "-n", "elsewhere")
	require.NoError(t, err)
	_, err = client.query("select-window", "-t", "watched:elsewhere")
	require.NoError(t, err)

	assert.False(t, client.IsClientAtPane(pane, time.Minute),
		"whoever is attached is looking at another window")
}

// TestIsClientAtPaneWithSomebodyWhoHasStoppedTyping covers the client that is still attached and
// looking at the window, having taken no key for longer than the window asked for.
func TestIsClientAtPaneWithSomebodyWhoHasStoppedTyping(t *testing.T) {
	client := testServer(t, "quiet")

	pane, err := client.query("display-message", "-p", "-t", "quiet:", "#{pane_id}")
	require.NoError(t, err)

	attachClient(t, client, "quiet")

	// tmux writes the time in whole seconds, so the wait has to clear one.
	time.Sleep(1100 * time.Millisecond)

	assert.False(t, client.IsClientAtPane(pane, time.Second))
	assert.True(t, client.IsClientAtPane(pane, time.Minute), "and is still there over a longer window")
}

// attachClient puts a real client on a session of the server under test, running it in a pane of
// a second server of its own.
func attachClient(t *testing.T, client *Client, session string) *Client {
	t.Helper()

	socket := client.server[1]
	outer := &Client{server: []string{"-L", socket + "-outer", "-f", "/dev/null"}}

	t.Cleanup(func() {
		_, _ = outer.query("kill-server")
	})

	_, err := outer.query("new-session", "-d", "-x", "80", "-y", "24", "--",
		"tmux", "-L", socket, "-f", "/dev/null", "attach", "-t", session)
	require.NoError(t, err, "could not start a client against the server under test")

	require.Eventually(t, func() bool {
		listed, err := client.query("list-clients", "-t", session, "-F", "#{client_name}")

		return err == nil && listed != ""
	}, 5*time.Second, 20*time.Millisecond, "no client ever attached to the server under test")

	return outer
}
