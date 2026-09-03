package tmux

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T, name string) *Client {
	t.Helper()

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	socket := "weave-test-" + strings.ReplaceAll(t.Name(), "/", "-")
	client := &Client{server: []string{"-L", socket}}

	t.Cleanup(func() {
		_, _ = client.query("kill-server")
	})

	_, err := client.query("new-session", "-d", "-s", name, "-n", name, "--", "sh")
	require.NoError(t, err, "could not start a tmux server to test against")

	return client
}

func windowNames(t *testing.T, c *Client, session string) []string {
	t.Helper()

	out, err := c.query("list-windows", "-t", session+":", "-F", "#{window_name}")
	require.NoError(t, err)

	return strings.Split(out, "\n")
}

// TestNewWindowAlongsideASameNamedWindow covers the panel's own case, where tmux matches a
// bare target against window names before session names and lands on the wrong one.
func TestNewWindowAlongsideASameNamedWindow(t *testing.T) {
	client := testServer(t, "weave")

	pane, err := client.NewWindow("weave", "agent-one", t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, pane)

	assert.Equal(t, []string{"weave", "agent-one"}, windowNames(t, client, "weave"))
}

// TestSessionClientWithNothingAttached checks a server nobody is attached to and a session
// that is not there both answer nothing, rather than a target tmux picked.
func TestSessionClientWithNothingAttached(t *testing.T) {
	client := testServer(t, "detached")

	assert.Empty(t, client.GetSessionClient("$0"))
	assert.Empty(t, client.GetSessionClient("$99"))
}

// TestOpenWindowWithoutAClient covers a server standing in for the user's own rather than the
// manager's, since that is the only side OpenWindow is ever pointed at.
func TestOpenWindowWithoutAClient(t *testing.T) {
	client := testServer(t, "host")

	require.Error(t, client.OpenWindow("", "editor", t.TempDir(), "sh"))
	require.Error(t, client.OpenWindow("/dev/ttys999", "editor", t.TempDir(), "sh"))

	assert.Equal(t, []string{"host"}, windowNames(t, client, "host"))
}

// TestNewWindowRepeatedly covers several agents launching into the same session, which is
// the ordinary case and the one that finds an index tmux was given rather than chose.
func TestNewWindowRepeatedly(t *testing.T) {
	client := testServer(t, "weave")

	for _, name := range []string{"agent-one", "agent-two", "agent-three"} {
		pane, err := client.NewWindow("weave", name, t.TempDir())
		require.NoError(t, err, "failed to open a window for %q", name)
		assert.NotEmpty(t, pane)
	}

	assert.Equal(t, []string{"weave", "agent-one", "agent-two", "agent-three"}, windowNames(t, client, "weave"))
}

// TestPaneOwners covers the batched lookup a whole listing goes through, where a pane
// that is gone has to be absent rather than reported under a stale stamp.
func TestPaneOwners(t *testing.T) {
	client := testServer(t, "weave")

	stamped, err := client.NewWindow("weave", "stamped", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, client.SetPaneOwner(stamped, "agent-one"))

	bare, err := client.NewWindow("weave", "bare", t.TempDir())
	require.NoError(t, err)

	killed, err := client.NewWindow("weave", "killed", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, client.SetPaneOwner(killed, "agent-two"))
	client.KillPane(killed)

	panes, err := client.GetPanes()
	require.NoError(t, err)

	assert.Equal(t, "agent-one", panes[stamped].Owner, "a stamped pane answers to its agent")
	assert.Empty(t, panes[bare].Owner, "an unstamped pane answers to nobody")
	assert.NotContains(t, panes, killed, "a pane that is gone is absent rather than stale")
	assert.Equal(t, client.GetPaneOwner(stamped), panes[stamped].Owner, "the batch agrees with the single lookup")
	assert.True(t, panes[stamped].IsRunningShell(), "a pane weave opened is a shell until something is launched in it")
}

// TestSiblingPanesFindsTheRestOfTheWindow covers a restarted panel finding what the last one
// left, which needs a real server to confirm tmux maps a pane id to the window holding it.
func TestSiblingPanesFindsTheRestOfTheWindow(t *testing.T) {
	client := testServer(t, "weave")

	panel, err := client.query("display-message", "-p", "-t", "weave:", "#{pane_id}")
	require.NoError(t, err)

	first := client.SplitRight(panel, "/tmp", "50%")
	second := client.SplitRight(panel, "/tmp", "50%")
	require.NotEmpty(t, first)
	require.NotEmpty(t, second)

	siblings := client.GetSiblingPanes(panel)

	assert.ElementsMatch(t, []string{first, second}, siblings, "every pane but the one asked about")
	assert.NotContains(t, siblings, panel, "the pane asked about is not its own sibling")
}

// TestSiblingPanesOfNothing covers the panel running outside tmux, where nothing is reclaimed
// rather than tmux being asked about the active pane in some other window.
func TestSiblingPanesOfNothing(t *testing.T) {
	client := testServer(t, "weave")

	assert.Empty(t, client.GetSiblingPanes(""))
}

// TestBreakPaneKeepsTheID covers a staged agent returning to its own window, where a break
// that renamed the pane would lose the agent the panel tracks by that id.
func TestBreakPaneKeepsTheID(t *testing.T) {
	client := testServer(t, "weave")

	panel, err := client.query("display-message", "-p", "-t", "weave:", "#{pane_id}")
	require.NoError(t, err)

	staged := client.SplitRight(panel, "/tmp", "50%")
	require.NotEmpty(t, staged)

	client.BreakPane(staged)

	assert.True(t, client.IsPaneAlive(staged), "the pane survives being broken out")
	assert.Empty(t, client.GetSiblingPanes(panel), "and is no longer in the panel's window")
	assert.Len(t, windowNames(t, client, "weave"), 2, "it is a window of its own now")
}
