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

// TestSplitsPlaceThePaneWhereTheyAreNamedFor pins which side each one opens on.
func TestSplitsPlaceThePaneWhereTheyAreNamedFor(t *testing.T) {
	tests := []struct {
		name  string
		split func(*Client, string, string) string
		want  string
	}{
		{
			name:  "to the right",
			split: func(c *Client, anchor, dir string) string { return c.SplitRight(anchor, dir, "50%") },
			want:  "right",
		},
		{
			name:  "to the left",
			split: func(c *Client, anchor, dir string) string { return c.SplitLeft(anchor, dir, "50%") },
			want:  "left",
		},
		{
			name:  "above",
			split: func(c *Client, anchor, dir string) string { return c.SplitAbove(anchor, dir, "50%") },
			want:  "above",
		},
		{
			name:  "below",
			split: func(c *Client, anchor, dir string) string { return c.SplitBelow(anchor, dir, "50%") },
			want:  "below",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testServer(t, "splits")
			anchor := paneAt(t, client, "splits", 0)

			opened := tt.split(client, anchor, t.TempDir())
			require.NotEmpty(t, opened, "the split answers with the pane it made")
			require.NotEqual(t, anchor, opened)

			assert.Equal(t, tt.want, sideOf(t, client, anchor, opened))
		})
	}
}

// sideOf works out where one pane sits against another from the two corners tmux reports, which
// is the only thing that says a split went the way it was asked for.
func sideOf(t *testing.T, c *Client, anchor, opened string) string {
	t.Helper()

	anchorLeft, anchorTop := paneCorner(t, c, anchor)
	openedLeft, openedTop := paneCorner(t, c, opened)

	switch {
	case openedLeft > anchorLeft:
		return "right"
	case openedLeft < anchorLeft:
		return "left"
	case openedTop < anchorTop:
		return "above"
	case openedTop > anchorTop:
		return "below"
	}

	return "on top of it"
}

func paneCorner(t *testing.T, c *Client, pane string) (left, top string) {
	t.Helper()

	out, err := c.query("display-message", "-p", "-t", pane, "#{pane_left},#{pane_top}")
	require.NoError(t, err)

	l, tp, _ := strings.Cut(strings.TrimSpace(out), ",")

	return l, tp
}

// TestSelectPaneMovesTheKeyboard pins the move that hands a pane the keys, which is how anything
// the person has to type into gets them once it is up.
func TestSelectPaneMovesTheKeyboard(t *testing.T) {
	client := testServer(t, "select")

	first := paneAt(t, client, "select", 0)
	second := client.SplitRight(first, t.TempDir(), "50%")
	require.NotEmpty(t, second)

	client.SelectPane(second)
	focused, _ := client.GetPaneFocus(second)
	assert.True(t, focused, "the pane that was selected holds the keys")

	wasFocused, _ := client.GetPaneFocus(first)
	assert.False(t, wasFocused, "and the one that had them has given them up")
}

// TestPaneFocusAnswersForBothHalvesAtOnce pins the two facts one query carries, focus and zoom
// moving together under a key bound on the server.
func TestPaneFocusAnswersForBothHalvesAtOnce(t *testing.T) {
	client := testServer(t, "focusask")

	first := paneAt(t, client, "focusask", 0)
	second := client.SplitRight(first, t.TempDir(), "50%")
	require.NotEmpty(t, second)

	client.SelectPane(second)

	focused, zoomed := client.GetPaneFocus(second)
	assert.True(t, focused)
	assert.False(t, zoomed, "a pane sharing its window is not filling it")

	_, err := client.query("resize-pane", "-Z", "-t", second)
	require.NoError(t, err)

	focused, zoomed = client.GetPaneFocus(second)
	assert.True(t, focused)
	assert.True(t, zoomed, "the zoomed pane is filling its window")

	unfocused, alsoZoomed := client.GetPaneFocus(first)
	assert.False(t, unfocused)
	assert.False(t, alsoZoomed, "the pane that is not active is not the one filling the window")
}

// TestPaneFocusOfNothingIsNotTheUsersPane pins the empty target, which tmux would otherwise
// answer for whichever pane the person is looking at.
func TestPaneFocusOfNothingIsNotTheUsersPane(t *testing.T) {
	client := testServer(t, "nopane")

	focused, zoomed := client.GetPaneFocus("")
	assert.False(t, focused)
	assert.False(t, zoomed)

	gone, goneZoomed := client.GetPaneFocus("%999")
	assert.False(t, gone)
	assert.False(t, goneZoomed, "a pane that has gone is not the one holding the keys")
}

// TestResizePaneSetsTheWidthItWasGiven pins the resize the panel sizes its own column with.
func TestResizePaneSetsTheWidthItWasGiven(t *testing.T) {
	client := testServer(t, "resize")

	_, err := client.query("resize-window", "-t", "resize:", "-x", "120", "-y", "40")
	require.NoError(t, err)

	first := paneAt(t, client, "resize", 0)
	require.NotEmpty(t, client.SplitRight(first, t.TempDir(), "50%"))

	client.ResizePane(first, "30")

	assert.Equal(t, "30", paneWidth(t, client, first))
}

func paneWidth(t *testing.T, c *Client, pane string) string {
	t.Helper()

	out, err := c.query("display-message", "-p", "-t", pane, "#{pane_width}")
	require.NoError(t, err)

	return strings.TrimSpace(out)
}

// TestPaneMeasurementsAnswerZeroForAPaneThatIsNotThere pins the fallback the panel lays itself
// out off.
func TestPaneMeasurementsAnswerZeroForAPaneThatIsNotThere(t *testing.T) {
	client := testServer(t, "measure")

	_, err := client.query("resize-window", "-t", "measure:", "-x", "100", "-y", "30")
	require.NoError(t, err)

	pane := paneAt(t, client, "measure", 0)
	assert.Equal(t, 100, client.GetPaneWidth(pane))
	assert.Equal(t, 30, client.GetPaneHeight(pane))

	for _, absent := range []string{"", "%999"} {
		assert.Zero(t, client.GetPaneWidth(absent), "width of %q", absent)
		assert.Zero(t, client.GetPaneHeight(absent), "height of %q", absent)
	}
}

// TestResizeWindowRefusesASizeThatIsNotOne pins the guard, tmux reading a zero as a request to
// fit the window to its client.
func TestResizeWindowRefusesASizeThatIsNotOne(t *testing.T) {
	client := testServer(t, "resizewin")
	pane := paneAt(t, client, "resizewin", 0)

	client.ResizeWindow(pane, 100, 30)
	require.Equal(t, 100, client.GetPaneWidth(pane))

	for _, size := range [][2]int{{0, 30}, {100, 0}, {-1, 30}} {
		client.ResizeWindow(pane, size[0], size[1])
		assert.Equal(t, 100, client.GetPaneWidth(pane), "%v left the window as it was", size)
		assert.Equal(t, 30, client.GetPaneHeight(pane))
	}

	client.ResizeWindow("", 50, 20)
	assert.Equal(t, 100, client.GetPaneWidth(pane), "a window nobody named is not resized")
}

// TestPaneOwnerIsStampedAndGivenBack pins the round trip the reconciler reads panes by, and the
// refusal to unstamp nothing, which would otherwise clear the option on the pane in view.
func TestPaneOwnerIsStampedAndGivenBack(t *testing.T) {
	client := testServer(t, "owner")
	pane := paneAt(t, client, "owner", 0)

	require.NoError(t, client.SetPaneOwner(pane, "agent-one"))
	assert.Equal(t, "agent-one", client.GetPaneOwner(pane))

	require.NoError(t, client.ClearPaneOwner(pane))
	assert.Empty(t, client.GetPaneOwner(pane), "the pane is nobody's again")

	require.Error(t, client.ClearPaneOwner(""), "a pane nobody named is not one to unstamp")
}

// TestPaneRunningCLIReadsTheForegroundProgram pins what says the harness weave typed has started,
// which is the foreground of the pane's terminal rather than the process tmux launched.
func TestPaneRunningCLIReadsTheForegroundProgram(t *testing.T) {
	client := testServer(t, "running")
	pane := paneAt(t, client, "running", 0)

	require.Eventually(t, func() bool {
		return !client.IsPaneRunningCLI(pane)
	}, 5*time.Second, 20*time.Millisecond, "a pane sitting at its shell is running no CLI")

	require.NoError(t, client.SendKeys(pane, "cat", "Enter"))

	require.Eventually(t, func() bool {
		return client.IsPaneRunningCLI(pane)
	}, 5*time.Second, 20*time.Millisecond, "the program typed into the pane is what it is running")

	for _, absent := range []string{"", "%999"} {
		assert.False(t, client.IsPaneRunningCLI(absent), "%q is not a pane running anything", absent)
	}
}

// TestDefaultShellIsWhatAPaneWouldBeParsedBy pins the option the doctor refuses a login shell
// weave cannot launch an agent under by.
func TestDefaultShellIsWhatAPaneWouldBeParsedBy(t *testing.T) {
	client := testServer(t, "shell")

	assert.Equal(t, "/bin/sh", client.GetDefaultShell())
}

// TestDetachSessionLeavesThePaneProcessesRunning pins that hiding the manager stops at the
// client.
func TestDetachSessionLeavesThePaneProcessesRunning(t *testing.T) {
	client := testServer(t, "detach")
	pane := paneAt(t, client, "detach", 0)

	client.DetachSession("detach")

	assert.Empty(t, client.GetSessionClient("detach"), "nothing is attached to it any more")

	panes, err := client.query("list-panes", "-t", "detach:", "-F", "#{pane_id}")
	require.NoError(t, err)
	assert.Contains(t, panes, pane, "the pane it was hiding is still there")
}

// TestClientPathFollowsTheCurrentWindow pins that a server no client is attached to still
// answers, with the current window's active pane rather than with nothing, so a caller cannot
// tell the two apart and the panel roots a new terminal wherever that pane is.
func TestClientPathFollowsTheCurrentWindow(t *testing.T) {
	client := testServer(t, "clientpath")

	dir := t.TempDir()
	_, err := client.NewWindow("clientpath", "elsewhere", dir)
	require.NoError(t, err)

	_, err = client.query("select-window", "-t", "clientpath:elsewhere")
	require.NoError(t, err)

	got, err := filepath.EvalSymlinks(client.GetClientPath())
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

// TestRespawnPaneRunsTheCommandInPlace covers how an agent's CLI is started, which is as the
// pane's own process rather than as a line typed at a shell.
func TestRespawnPaneRunsTheCommandInPlace(t *testing.T) {
	client := testServer(t, "respawn")
	pane := paneAt(t, client, "respawn", 0)
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")

	require.NoError(t, client.RespawnPane(pane, dir, "sh", "-c", "pwd > "+marker+"; exec sh"))

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "the command runs as the pane's process")

	where, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Equal(t, dir, strings.TrimSpace(string(where)), "and runs in the directory it was given")

	require.Error(t, client.RespawnPane("", dir, "sh"), "a pane nobody named is not one to respawn")
	require.Error(t, client.RespawnPane(pane, dir), "a respawn with no command is refused")
}
