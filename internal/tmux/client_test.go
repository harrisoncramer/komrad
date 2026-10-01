//go:build integration

package tmux

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"komrad/internal/panes"
	"komrad/lib/clock"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationQueryCarriesStderr checks a failed tmux call names the command and carries tmux's own
// complaint.
func TestIntegrationQueryCarriesStderr(t *testing.T) {
	_, lookErr := exec.LookPath("tmux")
	require.NoError(t, lookErr)

	_, err := NewClient("komrad-test-no-such-socket").GetPanes()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list-panes", "the failing command is named")
	assert.Contains(t, err.Error(), "komrad-test-no-such-socket", "so is the server it ran against")
	assert.Contains(t, err.Error(), "error connecting", "and tmux's own stderr comes through")
}

// TestIntegrationQueryGivesUpOnWedgedTmux covers a tmux that never answers, which would otherwise stall
// a whole reconcile pass with no word of why.
func TestIntegrationQueryGivesUpOnWedgedTmux(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "a tmux that never answers",
			script: "#!/bin/sh\nexec /bin/sleep 30\n",
		},
		{
			// A real tmux daemonizes a server holding the stdout it inherited, which keeps the
			// read open past the kill.
			name:   "a tmux that leaves a child holding its output",
			script: "#!/bin/sh\n/bin/sleep 30 &\nexec /bin/sleep 30\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", fakeTmux(t, tt.script))

			type result struct {
				out string
				err error
			}

			done := make(chan result, 1)
			go func() {
				out, err := (&Client{}).query("list-panes")
				done <- result{out: out, err: err}
			}()

			select {
			case got := <-done:
				require.Error(t, got.err, "giving up is a failure, so a caller reading state is not told the pane is gone")
				assert.Contains(t, got.err.Error(), "gave up waiting", "it returned because it ran out of time, not because tmux answered")
				assert.Contains(t, got.err.Error(), "list-panes", "the call that hung is named")
				assert.Empty(t, got.out, "and nothing half-read is passed off as an answer")
			case <-time.After(queryTimeout + 2*time.Second):
				t.Fatal("query did not return against a tmux that never answers")
			}
		})
	}
}

func fakeTmux(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755))

	return dir
}

func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return &buf
}

// TestIntegrationKillPaneIsQuietWhenThePaneIsAlreadyGone covers a delete and an archive racing the same
// pane, where the loser's kill-pane finds it gone and used to log a WARN.
func TestIntegrationKillPaneIsQuietWhenThePaneIsAlreadyGone(t *testing.T) {
	t.Setenv("PATH", fakeTmux(t, "#!/bin/sh\necho \"can't find pane: %13\" >&2\nexit 1\n"))
	buf := captureDefaultLog(t)

	(&Client{}).KillPane("%13")

	assert.Empty(t, buf.String(), "the pane being already gone is the wanted outcome, not a WARN")
}

// TestIntegrationKillPaneStillWarnsOnARealFailure guards the quiet case above from swallowing a failure
// that is not the pane being gone.
func TestIntegrationKillPaneStillWarnsOnARealFailure(t *testing.T) {
	t.Setenv("PATH", fakeTmux(t, "#!/bin/sh\necho 'error connecting to /tmp/tmux-501/komrad (No such file or directory)' >&2\nexit 1\n"))
	buf := captureDefaultLog(t)

	(&Client{}).KillPane("%13")

	assert.Contains(t, buf.String(), "a tmux command failed", "a real failure is not silenced")
}

// TestIntegrationIsRunningShellReadsMeasuredCommands pins the shell allowlist to what a pane actually reports.
func TestIntegrationIsRunningShellReadsMeasuredCommands(t *testing.T) {
	shells := []string{"sh", "bash", "dash", "ash", "zsh", "ksh", "csh", "tcsh", "fish", "nu", "-zsh", "-bash", "-dash"}
	for _, command := range shells {
		assert.True(t, PaneStatus{Command: command}.IsRunningShell(), "%q is a shell", command)
	}

	// Claude sets its own process title so a pane reports its version, and Codex reports the
	// wrapper script it was installed as.
	others := []string{"2.1.241", ".codex-wrapped", "codex", "claude", "node", "cat", ""}
	for _, command := range others {
		assert.False(t, PaneStatus{Command: command}.IsRunningShell(), "%q is not a shell", command)
	}
}

func testServer(t *testing.T, name string) *Client {
	t.Helper()

	_, lookErr := exec.LookPath("tmux")
	require.NoError(t, lookErr)

	socket := "komrad-test-" + strings.ReplaceAll(t.Name(), "/", "-")
	client := &Client{server: []string{"-L", socket, "-f", "/dev/null"}}

	t.Cleanup(func() {
		_, _ = client.query("kill-server")
	})

	_, err := client.query("new-session", "-d", "-s", name, "-n", name, "--", "sh")
	require.NoError(t, err, "could not start a tmux server to test against")

	// A pane reports whichever process holds its terminal, so a login shell reading the
	// developer's startup files reports those children rather than itself.
	_, err = client.query("set-option", "-g", "default-shell", "/bin/sh")
	require.NoError(t, err, "could not pin the test server's shell")
	_, err = client.query("set-option", "-g", "default-command", "sh")
	require.NoError(t, err, "could not pin the test server's shell")

	return client
}

func windowNames(t *testing.T, c *Client, session string) []string {
	t.Helper()

	out, err := c.query("list-windows", "-t", session+":", "-F", "#{window_name}")
	require.NoError(t, err)

	return strings.Split(out, "\n")
}

// TestIntegrationNewWindowAlongsideASameNamedWindow covers the panel's own case, where tmux matches a
// bare target against window names before session names and lands on the wrong one.
func TestIntegrationNewWindowAlongsideASameNamedWindow(t *testing.T) {
	client := testServer(t, "komrad")

	pane, err := client.NewWindow("komrad", "agent-one", t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, pane)

	assert.Equal(t, []string{"komrad", "agent-one"}, windowNames(t, client, "komrad"))
}

// TestIntegrationSessionClientWithNothingAttached checks a server nobody is attached to and a session that
// is not there both answer nothing.
func TestIntegrationSessionClientWithNothingAttached(t *testing.T) {
	client := testServer(t, "detached")

	assert.Empty(t, client.GetSessionClient("$0"))
	assert.Empty(t, client.GetSessionClient("$99"))
}

// TestIntegrationNewWindowRepeatedly covers several agents launching into the same session, which is
// the ordinary case and the one that finds an index tmux was given rather than chose.
func TestIntegrationNewWindowRepeatedly(t *testing.T) {
	client := testServer(t, "komrad")

	for _, name := range []string{"agent-one", "agent-two", "agent-three"} {
		pane, err := client.NewWindow("komrad", name, t.TempDir())
		require.NoError(t, err, "failed to open a window for %q", name)
		assert.NotEmpty(t, pane)
	}

	assert.Equal(t, []string{"komrad", "agent-one", "agent-two", "agent-three"}, windowNames(t, client, "komrad"))
}

// TestIntegrationPaneOwners covers the batched lookup a whole listing goes through, where a pane
// that is gone has to be absent rather than reported under a stale stamp.
func TestIntegrationPaneOwners(t *testing.T) {
	client := testServer(t, "komrad")

	stamped, err := client.NewWindow("komrad", "stamped", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, client.SetPaneOwner(stamped, "agent-one"))

	bare, err := client.NewWindow("komrad", "bare", t.TempDir())
	require.NoError(t, err)

	killed, err := client.NewWindow("komrad", "killed", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, client.SetPaneOwner(killed, "agent-two"))
	client.KillPane(killed)

	panes, err := client.GetPanes()
	require.NoError(t, err)

	assert.Equal(t, "agent-one", panes[stamped].Owner, "a stamped pane answers to its agent")
	assert.Empty(t, panes[bare].Owner, "an unstamped pane answers to nobody")
	assert.NotContains(t, panes, killed, "a pane that is gone is absent rather than stale")
	assert.Equal(t, client.GetPaneOwner(stamped), panes[stamped].Owner, "the batch agrees with the single lookup")
	assert.True(t, panes[stamped].IsRunningShell(), "a pane komrad opened is a shell until something is launched in it")
}

// TestIntegrationSiblingPanesFindsTheRestOfTheWindow covers a restarted panel finding what the last one
// left, which needs a real server to confirm tmux maps a pane id to the window holding it.
func TestIntegrationSiblingPanesFindsTheRestOfTheWindow(t *testing.T) {
	client := testServer(t, "komrad")

	panel, err := client.query("display-message", "-p", "-t", "komrad:", "#{pane_id}")
	require.NoError(t, err)

	first := client.SplitRight(panel, "/tmp", "50%")
	second := client.SplitRight(panel, "/tmp", "50%")
	require.NotEmpty(t, first)
	require.NotEmpty(t, second)

	siblings := client.GetSiblingPanes(panel)

	assert.ElementsMatch(t, []string{first, second}, siblings, "every pane but the one asked about")
	assert.NotContains(t, siblings, panel, "the pane asked about is not its own sibling")
}

// TestIntegrationSiblingPanesOfNothing covers the panel running outside tmux, where nothing is reclaimed
// rather than tmux being asked about the active pane in some other window.
func TestIntegrationSiblingPanesOfNothing(t *testing.T) {
	client := testServer(t, "komrad")

	assert.Empty(t, client.GetSiblingPanes(""))
}

// TestIntegrationBreakPaneKeepsTheID covers a staged agent returning to its own window, where a break
// that renamed the pane would lose the agent the panel tracks by that id.
func TestIntegrationBreakPaneKeepsTheID(t *testing.T) {
	client := testServer(t, "komrad")

	panel, err := client.query("display-message", "-p", "-t", "komrad:", "#{pane_id}")
	require.NoError(t, err)

	staged := client.SplitRight(panel, "/tmp", "50%")
	require.NotEmpty(t, staged)

	client.BreakPane(staged)

	assert.True(t, client.IsPaneAlive(staged), "the pane survives being broken out")
	assert.Empty(t, client.GetSiblingPanes(panel), "and is no longer in the panel's window")
	assert.Len(t, windowNames(t, client, "komrad"), 2, "it is a window of its own now")
}

// TestIntegrationSplitsPlaceThePaneWhereTheyAreNamedFor pins which side each one opens on.
func TestIntegrationSplitsPlaceThePaneWhereTheyAreNamedFor(t *testing.T) {
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

// TestIntegrationSelectPaneMovesTheKeyboard pins the move that hands a pane the keys.
func TestIntegrationSelectPaneMovesTheKeyboard(t *testing.T) {
	client := testServer(t, "select")

	first := paneAt(t, client, "select", 0)
	second := client.SplitRight(first, t.TempDir(), "50%")
	require.NotEmpty(t, second)

	client.SelectPane(second)
	focused, _ := client.GetPaneFocus(second)
	assert.True(t, focused, "the pane that was selected holds the keys")

	wasFocused, _ := client.GetPaneFocus(first)
	assert.False(t, wasFocused, "and the one that had them has given them up")
	assert.Equal(t, second, client.GetActivePane(first), "the window names the pane holding the keys from any pane in it")
}

// TestIntegrationPaneFocusAnswersForBothHalvesAtOnce pins the two facts one query carries, focus and zoom
// moving together under a key bound on the server.
func TestIntegrationPaneFocusAnswersForBothHalvesAtOnce(t *testing.T) {
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

// TestIntegrationPaneFocusOfNothingIsNotTheUsersPane pins the empty target, which tmux would otherwise
// answer for whichever pane the person is looking at.
func TestIntegrationPaneFocusOfNothingIsNotTheUsersPane(t *testing.T) {
	client := testServer(t, "nopane")

	focused, zoomed := client.GetPaneFocus("")
	assert.False(t, focused)
	assert.False(t, zoomed)

	gone, goneZoomed := client.GetPaneFocus("%999")
	assert.False(t, gone)
	assert.False(t, goneZoomed, "a pane that has gone is not the one holding the keys")
}

// TestIntegrationResizePaneSetsTheWidthItWasGiven pins the resize the panel sizes its own column with.
func TestIntegrationResizePaneSetsTheWidthItWasGiven(t *testing.T) {
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

// TestIntegrationPaneMeasurementsAnswerZeroForAPaneThatIsNotThere pins the fallback the panel lays itself
// out off.
func TestIntegrationPaneMeasurementsAnswerZeroForAPaneThatIsNotThere(t *testing.T) {
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

// TestIntegrationWindowWidthIsUnmovedByASplitOrAZoom pins the number that tells a dragged divider apart
// from a resized terminal.
func TestIntegrationWindowWidthIsUnmovedByASplitOrAZoom(t *testing.T) {
	client := testServer(t, "windowwidth")

	_, err := client.query("resize-window", "-t", "windowwidth:", "-x", "200", "-y", "40")
	require.NoError(t, err)

	sidebar := paneAt(t, client, "windowwidth", 0)
	require.Equal(t, 200, client.GetWindowWidth(sidebar))

	stage := client.SplitRight(sidebar, t.TempDir(), "149")
	require.NotEmpty(t, stage)
	assert.Equal(t, 200, client.GetWindowWidth(sidebar), "a split divides the window rather than changing it")
	require.Equal(t, 50, client.GetPaneWidth(sidebar))

	client.MovePanes([]panes.Move{panes.Zoom(sidebar, true)})
	require.Equal(t, 200, client.GetPaneWidth(sidebar), "the zoom hands the sidebar the whole window")
	assert.Equal(t, 200, client.GetWindowWidth(sidebar), "which leaves the window the size it was")
	assert.Equal(t, 149, client.GetPaneWidth(stage), "and leaves the pane it covers alone")

	for _, absent := range []string{"", "%999"} {
		assert.Zero(t, client.GetWindowWidth(absent), "window width of %q", absent)
	}
}

// TestIntegrationResizeWindowRefusesASizeThatIsNotOne pins the guard, tmux reading a zero as a request to
// fit the window to its client.
func TestIntegrationResizeWindowRefusesASizeThatIsNotOne(t *testing.T) {
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

// TestIntegrationPaneOwnerIsStamped pins the round trip the reconciler reads panes by.
func TestIntegrationPaneOwnerIsStamped(t *testing.T) {
	client := testServer(t, "owner")
	pane := paneAt(t, client, "owner", 0)

	require.NoError(t, client.SetPaneOwner(pane, "agent-one"))
	assert.Equal(t, "agent-one", client.GetPaneOwner(pane))
}

// TestIntegrationPaneRunningCLIReadsTheForegroundProgram pins what says the harness komrad typed has started,
// which is the foreground of the pane's terminal rather than the process tmux launched.
func TestIntegrationPaneRunningCLIReadsTheForegroundProgram(t *testing.T) {
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

		running, err := client.CheckPaneRunningCLI(absent)
		require.NoError(t, err, "a pane that is gone is an answer rather than a failure")
		assert.False(t, running)
	}
}

// TestIntegrationDefaultShellIsWhatAPaneWouldBeParsedBy pins the option the doctor refuses a login shell
// komrad cannot launch an agent under by.
func TestIntegrationDefaultShellIsWhatAPaneWouldBeParsedBy(t *testing.T) {
	client := testServer(t, "shell")

	assert.Equal(t, "/bin/sh", client.GetDefaultShell())
}

// TestIntegrationDetachSessionLeavesThePaneProcessesRunning pins that hiding the manager stops at the
// client.
func TestIntegrationDetachSessionLeavesThePaneProcessesRunning(t *testing.T) {
	client := testServer(t, "detach")
	pane := paneAt(t, client, "detach", 0)

	client.DetachSession("detach")

	assert.Empty(t, client.GetSessionClient("detach"), "nothing is attached to it any more")

	panes, err := client.query("list-panes", "-t", "detach:", "-F", "#{pane_id}")
	require.NoError(t, err)
	assert.Contains(t, panes, pane, "the pane it was hiding is still there")
}

// TestIntegrationClientPathFollowsTheCurrentWindow pins that a server no client is attached to still
// answers, with the current window's active pane rather than with nothing, so a caller cannot
// tell the two apart and the panel roots a new terminal wherever that pane is.
func TestIntegrationClientPathFollowsTheCurrentWindow(t *testing.T) {
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

// TestIntegrationRespawnPaneRunsTheCommandInPlace covers how an agent's CLI is started.
func TestIntegrationRespawnPaneRunsTheCommandInPlace(t *testing.T) {
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

// TestIntegrationHostRecord drives two real tmux servers and checks everything the panel writes about the
// user's server stays on its own, which a fake could only assert against itself.
func TestIntegrationHostRecord(t *testing.T) {
	hostSocket := newTestServer(t, "host")
	managerSocket := newTestServer(t, "manager")

	manager := NewClient(managerSocket)

	assert.Empty(t, manager.GetHostSocket(), "before an open there is no user server to name")
	assert.Empty(t, manager.GetHostPath(), "and no directory a new agent could inherit")

	manager.RecordHostSocket(socketPath(t, hostSocket))
	assert.Equal(t, socketPath(t, hostSocket), manager.GetHostSocket(),
		"the open command records the server it was pressed on for everything else to read")
	assert.Empty(t, tmuxRun(t, hostSocket, "show-option", "-gqv", hostSocketOption),
		"and records it on its own server, out of reach of a second user server")

	manager.RecordHostPath("/tmp/where-the-user-was")
	assert.Equal(t, "/tmp/where-the-user-was", manager.GetHostPath(),
		"the panel reads the directory back rather than asking a server it does not run")

	manager.RecordHostPath("")
	assert.Empty(t, manager.GetHostPath(),
		"and an open with nowhere to read forgets the last answer rather than repeating it")
}

// TestIntegrationClientAtPath checks a client addressed by socket path reaches the same server as one
// addressed by name, which matters because the TMUX variable carries only a path.
func TestIntegrationClientAtPath(t *testing.T) {
	socket := newTestServer(t, "path")

	byName := NewClient(socket)
	byName.SetGlobal("@probe", "here")

	byPath := NewClientAtPath(socketPath(t, socket))
	assert.Equal(t, "here", byPath.ShowGlobal("@probe"))
}

func newTestServer(t *testing.T, role string) string {
	t.Helper()
	_, err := exec.LookPath("tmux")
	require.NoError(t, err)

	socket := "cca-" + role + "-" + filepath.Base(t.TempDir())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	cmd := exec.Command("tmux", "-L", socket, "-f", "/dev/null", "new-session", "-d", "-s", role, "-c", t.TempDir())
	require.NoError(t, cmd.Run(), "failed to start the %s tmux server", role)

	return socket
}

func socketPath(t *testing.T, socket string) string {
	t.Helper()

	return tmuxRun(t, socket, "display-message", "-p", "#{socket_path}")
}

func tmuxRun(t *testing.T, socket string, args ...string) string {
	t.Helper()

	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	require.NoError(t, err, "tmux %v", args)

	return strings.TrimSpace(string(out))
}

// TestIntegrationPaneInAModeTakesNoKeys proves that nothing sent to a pane in a mode arrives, literally or
// not.
func TestIntegrationPaneInAModeTakesNoKeys(t *testing.T) {
	client := testServer(t, "komrad")

	pane, err := client.query("display-message", "-p", "-t", "komrad:", "#{pane_id}")
	require.NoError(t, err)

	received := filepath.Join(t.TempDir(), "received")
	sendText(t, client, pane, `while IFS= read -r line; do printf '%s\n' "$line" >> `+received+`; done`)
	require.NoError(t, client.SendKeys(pane, "Enter"))

	sendText(t, client, pane, "before-the-mode")
	require.NoError(t, client.SendKeys(pane, "Enter"))
	waitForLine(t, received, "before-the-mode")
	assert.False(t, client.IsPaneInMode(pane), "a pane nobody has scrolled back in is in no mode")

	_, err = client.query("copy-mode", "-t", pane)
	require.NoError(t, err)
	assert.True(t, client.IsPaneInMode(pane), "a pane scrolled back is in a mode")

	if _, err := client.query("send-keys", "-t", pane, "-l", "--", "swallowed-by-the-mode"); err != nil {
		t.Logf("tmux refused the text while the pane was in a mode: %v", err)
	}
	if err := client.SendKeys(pane, "Enter"); err != nil {
		t.Logf("tmux refused the newline while the pane was in a mode: %v", err)
	}

	_, err = client.query("copy-mode", "-q", "-t", pane)
	require.NoError(t, err)
	sendText(t, client, pane, "after-the-mode")
	require.NoError(t, client.SendKeys(pane, "Enter"))
	waitForLine(t, received, "after-the-mode")

	assert.NotContains(t, readFile(t, received), "swallowed-by-the-mode",
		"nothing sent while the pane was in a mode reached the program")
}

func waitForLine(t *testing.T, path, want string) {
	t.Helper()

	for range 100 {
		if strings.Contains(readFile(t, path), want) {
			return
		}

		clock.Sleep(t.Context(), 50*time.Millisecond)
	}

	t.Fatalf("the program never received %q; it holds:\n%s", want, readFile(t, path))
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	out, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("could not read what the program received: %v", err)
	}

	return string(out)
}

// sendText types a string into a pane literally, a word naming a key included.
func sendText(t *testing.T, client *Client, pane, text string) {
	t.Helper()

	_, err := client.query("send-keys", "-t", pane, "-l", "--", text)
	require.NoError(t, err)
}

// TestIntegrationInsetPanelWindowLeavesAGutterPastEveryClient pins the hook arithmetic, a client
// attaching and then resizing each leaving the panel's window a column and a row short of it.
func TestIntegrationInsetPanelWindowLeavesAGutterPastEveryClient(t *testing.T) {
	client := testServer(t, "inset")
	client.RecordPanelPane(paneAt(t, client, "inset", 0))
	client.InsetPanelWindow("inset")

	attach := exec.Command("tmux", append(client.server, "attach", "-t", "inset")...)
	attach.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(attach, &pty.Winsize{Cols: 100, Rows: 30})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = attach.Process.Kill()
		_ = attach.Wait()
		_ = terminal.Close()
	})
	go func() {
		_, _ = io.Copy(io.Discard, terminal)
	}()

	windowSize := func() string {
		out, err := client.query("display-message", "-p", "-t", "inset:", "#{window_width}x#{window_height}")
		require.NoError(t, err)

		return out
	}

	assert.Eventually(t, func() bool {
		return windowSize() == "99x29"
	}, 5*time.Second, 20*time.Millisecond, "attaching at 100x30 leaves the window a gutter short")

	require.NoError(t, pty.Setsize(terminal, &pty.Winsize{Cols: 120, Rows: 40}))

	assert.Eventually(t, func() bool {
		return windowSize() == "119x39"
	}, 5*time.Second, 20*time.Millisecond, "resizing to 120x40 keeps the gutter")
}

// TestIntegrationShownPanesListsWhatTheWindowShows covers a split window, a zoomed one, and no pane.
func TestIntegrationShownPanesListsWhatTheWindowShows(t *testing.T) {
	client := testServer(t, "shown")

	_, err := client.query("resize-window", "-t", "shown:", "-x", "100", "-y", "30")
	require.NoError(t, err)

	first := paneAt(t, client, "shown", 0)
	second := client.SplitRight(first, t.TempDir(), "50")
	require.NotEmpty(t, second)

	assert.Equal(t, []panes.Rect{
		{
			Pane:   first,
			Left:   0,
			Top:    0,
			Width:  49,
			Height: 30,
		},
		{
			Pane:   second,
			Left:   50,
			Top:    0,
			Width:  50,
			Height: 30,
		},
	}, client.GetShownPanes(first), "a split window shows both panes")

	_, err = client.query("resize-pane", "-Z", "-t", second)
	require.NoError(t, err)

	assert.Equal(t, []panes.Rect{{
		Pane:   second,
		Left:   0,
		Top:    0,
		Width:  100,
		Height: 30,
	}},
		client.GetShownPanes(first), "a zoomed window shows only the zoomed pane")

	assert.Empty(t, client.GetShownPanes(""))
}

// TestIntegrationCaptureScreenKeepsTrailingCells covers spaces printed after the text surviving the capture.
func TestIntegrationCaptureScreenKeepsTrailingCells(t *testing.T) {
	client := testServer(t, "capture")
	pane := paneAt(t, client, "capture", 0)

	require.NoError(t, client.RespawnPane(pane, t.TempDir(), "sh", "-c", "printf 'ab   '; exec sleep 60"))

	require.Eventually(t, func() bool {
		return strings.HasPrefix(client.CaptureScreen(pane), "ab")
	}, 5*time.Second, 20*time.Millisecond)

	assert.True(t, strings.HasPrefix(client.CaptureScreen(pane), "ab   "), "the spaces after the text are kept")
}
