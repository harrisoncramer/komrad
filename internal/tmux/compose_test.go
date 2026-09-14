//go:build integration

package tmux

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"weave/internal/bintest"
)

// readlineShell is a shell whose line editing is the same emacs binding family both harnesses
// implement, which is the closest a test gets to a real input box.
const readlineShell = "/bin/bash"

// TestPasteArrivesWrappedInTheMarkers covers the reason a paste is used at all, a program in a
// vi keymap running typed characters as commands rather than taking them as text.
func TestPasteArrivesWrappedInTheMarkers(t *testing.T) {
	client := testServer(t, "paste")
	pane, received := rawPane(t, client, "paste", true)

	require.NoError(t, client.Paste(pane, "weave: 1 piece(s) of unread mail"))

	waitForLine(t, received, "^[[200~weave: 1 piece(s) of unread mail^[[201~")
}

// TestPasteAndSubmitSendsTheNewlineBehindTheText covers the order the two arrive in, the
// program taking a newline ahead of the text as a submission of whatever it already held.
func TestPasteAndSubmitSendsTheNewlineBehindTheText(t *testing.T) {
	client := testServer(t, "submit")
	pane, received := rawPane(t, client, "submit", true)

	require.NoError(t, client.PasteAndSubmit(pane, "read your mail"))

	waitForLine(t, received, "^[[200~read your mail^[[201~^M")
}

// TestPasteAndSubmitIsOneInvocation covers the reason the newline rides along with the paste,
// a second invocation leaving a gap for whatever is typed at the pane meanwhile.
func TestPasteAndSubmitIsOneInvocation(t *testing.T) {
	args := pasteArgs("buffer", "%9", "read your mail", ";", "send-keys", "-t", "%9", "Enter")

	assert.Equal(t, []string{
		"set-buffer", "-b", "buffer", "--", "read your mail",
		";", "paste-buffer", "-p", "-d", "-b", "buffer", "-t", "%9",
		";", "send-keys", "-t", "%9", "Enter",
	}, args)
}

// TestPasteWithoutTheModeArrivesAsPlainText covers a program that never asked for the markers,
// which takes the text as though it had been typed.
func TestPasteWithoutTheModeArrivesAsPlainText(t *testing.T) {
	client := testServer(t, "unbracketed")
	pane, received := rawPane(t, client, "unbracketed", false)

	require.NoError(t, client.Paste(pane, "typed after all"))

	waitForLine(t, received, "typed after all")
	assert.NotContains(t, readFile(t, received), "^[[200~", "no markers reached a program that never asked for them")
}

// TestPasteLeavesNoBufferBehind covers the buffer the paste is loaded through, which a person
// pasting at the pane themselves would otherwise find weave's text in.
func TestPasteLeavesNoBufferBehind(t *testing.T) {
	client := testServer(t, "buffers")
	pane, received := rawPane(t, client, "buffers", true)

	require.NoError(t, client.Paste(pane, "gone once pasted"))
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
	pane, received := rawPane(t, client, "cut", true)

	require.NoError(t, client.CutLine(pane))

	waitForLine(t, received, "^E^U")
}

// TestYankLineSendsTheYank covers the other half of the round trip.
func TestYankLineSendsTheYank(t *testing.T) {
	client := testServer(t, "yank")
	pane, received := rawPane(t, client, "yank", true)

	require.NoError(t, client.YankLine(pane))

	waitForLine(t, received, "^Y")
}

// TestComposingRefusesWhatItCannotSend covers the arguments that would otherwise reach tmux as a
// paste of nothing or a send to whichever pane happens to be current.
func TestComposingRefusesWhatItCannotSend(t *testing.T) {
	client := testServer(t, "refusals")

	require.Error(t, client.Paste("", "text"))
	require.Error(t, client.Paste("%0", ""))
	require.Error(t, client.PasteAndSubmit("", "text"))
	require.Error(t, client.PasteAndSubmit("%0", ""))
	require.Error(t, client.CutLine(""))
	require.Error(t, client.YankLine(""))
}

// rawPane is a pane whose terminal passes the control characters through rather than acting on
// them itself, alongside the file it writes what it receives to.
func rawPane(t *testing.T, client *Client, session string, bracketed bool) (pane, received string) {
	t.Helper()

	pane, err := client.query("display-message", "-p", "-t", session+":", "#{pane_id}")
	require.NoError(t, err)

	// tmux wraps a paste in the markers only for a pane whose program has asked for them, which
	// it learns from the program's own output rather than from anything weave says.
	enable := ""
	if bracketed {
		enable = `printf '\033[?2004h'; `
	}

	received = filepath.Join(t.TempDir(), "received")
	require.NoError(t, client.SendText(pane, enable+"stty raw -echo && cat -uv > "+received))
	require.NoError(t, client.SendKeys(pane, "Enter"))

	// The redirection is what says the shell has reached cat, since nothing typed before that
	// would be written anywhere.
	require.NoError(t, client.SendText(pane, "started"))
	waitForLine(t, received, "started")

	return pane, received
}

// TestCutAndYankRoundTripsThroughReadline covers what the byte tests cannot, a line editor
// clearing its own box whole and handing the text back, which is the whole reason the pair
// exists.
func TestCutAndYankRoundTripsThroughReadline(t *testing.T) {
	bintest.Require(t, readlineShell)

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
