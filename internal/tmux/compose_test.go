//go:build integration

package tmux

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readlineShell is a shell whose line editing is the same emacs binding family both harnesses
// implement, which is the closest a test gets to a real input box.
const readlineShell = "/bin/bash"

// TestPasteAndSubmitSendsTheText covers the text and Enter arriving together without
// bracketed-paste markers.
func TestPasteAndSubmitSendsTheText(t *testing.T) {
	client := testServer(t, "submit")
	pane, received := rawPane(t, client, "submit")

	require.NoError(t, client.PasteAndSubmit(pane, "read your mail"))

	waitForLine(t, received, "read your mail^M")
	assert.NotContains(t, readFile(t, received), "^[[200~", "submit must not wrap in paste markers")
}

// TestPasteAndSubmitIsOneInvocation covers the paste and Enter riding in a single tmux call
// so nothing typed at the pane can land between them.
func TestPasteAndSubmitIsOneInvocation(t *testing.T) {
	args := pasteArgs("buffer", "%9", "read your mail", ";", "send-keys", "-t", "%9", "Enter")

	assert.Equal(t, []string{
		"set-buffer", "-b", "buffer", "--", "read your mail",
		";", "paste-buffer", "-d", "-b", "buffer", "-t", "%9",
		";", "send-keys", "-t", "%9", "Enter",
	}, args)
}

// TestPasteLeavesNoBufferBehind covers the buffer the paste is loaded through, which a person
// pasting at the pane themselves would otherwise find weave's text in.
func TestPasteLeavesNoBufferBehind(t *testing.T) {
	client := testServer(t, "buffers")
	pane, received := rawPane(t, client, "buffers")

	require.NoError(t, client.PasteAndSubmit(pane, "gone once pasted"))
	waitForLine(t, received, "gone once pasted")

	out, err := client.query("list-buffers")
	require.NoError(t, err)
	assert.NotContains(t, out, pasteBufferPrefix, "the buffer the paste was loaded through was dropped")
}

// TestPastesAtOnceHoldTheirOwnBuffers covers two pastes overlapping, a name they shared meaning
// one text overwrites the other and the loser's paste finds its buffer already dropped.
func TestPastesAtOnceHoldTheirOwnBuffers(t *testing.T) {
	first := pasteArgs(pasteBufferName(), "%1", "for the first pane")
	second := pasteArgs(pasteBufferName(), "%2", "for the second pane")

	assert.NotEqual(t, first[2], second[2], "each paste names a buffer of its own")
}

// TestCutLineCarriesTheCursorFirst covers a kill taking only what is behind the cursor, which is
// what leaves the tail of a line behind.
func TestCutLineCarriesTheCursorFirst(t *testing.T) {
	client := testServer(t, "cut")
	pane, received := rawPane(t, client, "cut")

	require.NoError(t, client.CutLine(pane))

	waitForLine(t, received, "^E^U")
}

// TestYankLineSendsTheYank covers the other half of the round trip.
func TestYankLineSendsTheYank(t *testing.T) {
	client := testServer(t, "yank")
	pane, received := rawPane(t, client, "yank")

	require.NoError(t, client.YankLine(pane))

	waitForLine(t, received, "^Y")
}

// TestComposingRefusesWhatItCannotSend covers the arguments that would otherwise reach tmux as a
// paste of nothing or a send to whichever pane happens to be current.
func TestComposingRefusesWhatItCannotSend(t *testing.T) {
	client := testServer(t, "refusals")

	require.Error(t, client.PasteAndSubmit("", "text"))
	require.Error(t, client.PasteAndSubmit("%0", ""))
	require.Error(t, client.CutLine(""))
	require.Error(t, client.YankLine(""))
}

// rawPane is a pane whose terminal passes the control characters through rather than acting on
// them itself, alongside the file it writes what it receives to.
func rawPane(t *testing.T, client *Client, session string) (pane, received string) {
	t.Helper()

	pane, err := client.query("display-message", "-p", "-t", session+":", "#{pane_id}")
	require.NoError(t, err)

	received = filepath.Join(t.TempDir(), "received")
	require.NoError(t, client.SendText(pane, "stty raw -echo && cat -uv > "+received))
	require.NoError(t, client.SendKeys(pane, "Enter"))

	require.NoError(t, client.SendText(pane, "started"))
	waitForLine(t, received, "started")

	return pane, received
}

// TestCutAndYankRoundTripsThroughReadline covers what the byte tests cannot, a line editor
// clearing its own box whole and handing the text back, which is the whole reason the pair
// exists.
func TestCutAndYankRoundTripsThroughReadline(t *testing.T) {
	_, err := exec.LookPath(readlineShell)
	require.NoError(t, err)

	client := testServer(t, "readline")

	pane, err := client.query("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "readline:",
		"--", readlineShell, "--norc", "--noprofile", "-i")
	require.NoError(t, err)

	waitForFrame(t, client, pane, "$")
	prompt := lastLine(client.CapturePane(pane))

	require.NoError(t, client.SendText(pane, "echo hello world"))
	waitForFrame(t, client, pane, "echo hello world")

	// The cursor is left mid-line, which is where a kill on its own leaves the tail behind.
	require.NoError(t, client.SendKeys(pane, "Left", "Left", "Left"))

	require.NoError(t, client.CutLine(pane))
	waitForLastLine(t, client, pane, prompt)

	require.NoError(t, client.YankLine(pane))
	waitForFrame(t, client, pane, "echo hello world")
}

func waitForFrame(t *testing.T, client *Client, pane, want string) {
	t.Helper()

	for range 100 {
		if strings.Contains(client.CapturePane(pane), want) {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the pane never showed %q; it holds:\n%s", want, client.CapturePane(pane))
}

// waitForLastLine waits for the bottom line of a pane to hold nothing but what is wanted, which
// is how a box with a tail left in it is told from an empty one.
func waitForLastLine(t *testing.T, client *Client, pane, want string) {
	t.Helper()

	for range 100 {
		if lastLine(client.CapturePane(pane)) == want {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the pane's bottom line never came to %q; it holds:\n%s", want, client.CapturePane(pane))
}

func lastLine(frame string) string {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}
