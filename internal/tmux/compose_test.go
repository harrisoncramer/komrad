package tmux

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	args := pasteArgs("%9", "read your mail", ";", "send-keys", "-t", "%9", "Enter")

	assert.Equal(t, []string{
		"set-buffer", "-b", pasteBuffer, "--", "read your mail",
		";", "paste-buffer", "-p", "-d", "-b", pasteBuffer, "-t", "%9",
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
	assert.NotContains(t, out, pasteBuffer, "the buffer the paste was loaded through was dropped")
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
