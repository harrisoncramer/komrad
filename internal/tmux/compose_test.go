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
// implement.
const readlineShell = "/bin/bash"

// TestIntegrationPasteAndSubmitSendsTheText covers the text reaching a pane wrapped in paste markers.
func TestIntegrationPasteAndSubmitSendsTheText(t *testing.T) {
	client := testServer(t, "submit")
	pane, received := rawPane(t, client, "submit")

	require.NoError(t, client.PasteAndSubmit(pane, "read your mail"))

	waitForLine(t, received, "^[[200~read your mail^[[201~^M")
}

// TestIntegrationPasteAndSubmitDropsControlCharacters covers a control character in the text never
// reaching the pane.
func TestIntegrationPasteAndSubmitDropsControlCharacters(t *testing.T) {
	client := testServer(t, "controls")
	pane, received := rawPane(t, client, "controls")

	require.NoError(t, client.PasteAndSubmit(pane, "read\x1b your\x7f mail\ttoday\nplease"))

	waitForLine(t, received, "^[[200~read your mail\ttoday^Mplease^[[201~^M")
}

// TestIntegrationStripControlsKeepsTheText pins which characters go and which stay.
func TestIntegrationStripControlsKeepsTheText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "plain text",
			text: "read your mail",
			want: "read your mail",
		},
		{
			name: "an escape",
			text: "read\x1b[201~ mail",
			want: "read[201~ mail",
		},
		{
			name: "a delete",
			text: "read\x7f mail",
			want: "read mail",
		},
		{
			name: "a C1 control",
			text: "read\u0085 mail",
			want: "read mail",
		},
		{
			name: "a carriage return",
			text: "read\r\nmail",
			want: "read\nmail",
		},
		{
			name: "a newline and a tab",
			text: "read\n\tmail",
			want: "read\n\tmail",
		},
		{
			name: "text past ASCII",
			text: "lire le courrier, s'il vous plaît ✉",
			want: "lire le courrier, s'il vous plaît ✉",
		},
		{
			name: "nothing but controls",
			text: "\x1b\x07",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripControls(tt.text))
		})
	}
}

// TestIntegrationPasteAndSubmitIsOneInvocation covers the paste and Enter riding in a single tmux call
// so nothing typed at the pane can land between them.
func TestIntegrationPasteAndSubmitIsOneInvocation(t *testing.T) {
	args := pasteArgs("buffer", "%9", "read your mail", ";", "send-keys", "-t", "%9", "Enter")

	assert.Equal(t, []string{
		"set-buffer", "-b", "buffer", "--", "read your mail",
		";", "paste-buffer", "-d", "-p", "-b", "buffer", "-t", "%9",
		";", "send-keys", "-t", "%9", "Enter",
	}, args)
}

// TestIntegrationPasteLeavesNoBufferBehind covers the buffer the paste is loaded through, which a person
// pasting at the pane themselves would otherwise find komrade's text in.
func TestIntegrationPasteLeavesNoBufferBehind(t *testing.T) {
	client := testServer(t, "buffers")
	pane, received := rawPane(t, client, "buffers")

	require.NoError(t, client.PasteAndSubmit(pane, "gone once pasted"))
	waitForLine(t, received, "gone once pasted")

	out, err := client.query("list-buffers")
	require.NoError(t, err)
	assert.NotContains(t, out, pasteBufferPrefix, "the buffer the paste was loaded through was dropped")
}

// TestIntegrationPastesAtOnceHoldTheirOwnBuffers covers two pastes overlapping, a name they shared meaning
// one text overwrites the other and the loser's paste finds its buffer already dropped.
func TestIntegrationPastesAtOnceHoldTheirOwnBuffers(t *testing.T) {
	first := pasteArgs(pasteBufferName(), "%1", "for the first pane")
	second := pasteArgs(pasteBufferName(), "%2", "for the second pane")

	assert.NotEqual(t, first[2], second[2], "each paste names a buffer of its own")
}

// TestIntegrationCutLineCarriesTheCursorFirst covers a kill taking only what is behind the cursor, which is
// what leaves the tail of a line behind.
func TestIntegrationCutLineCarriesTheCursorFirst(t *testing.T) {
	client := testServer(t, "cut")
	pane, received := rawPane(t, client, "cut")

	require.NoError(t, client.CutLine(pane))

	waitForLine(t, received, "^E^U")
}

// TestIntegrationYankLineSendsTheYank covers the other half of the round trip.
func TestIntegrationYankLineSendsTheYank(t *testing.T) {
	client := testServer(t, "yank")
	pane, received := rawPane(t, client, "yank")

	require.NoError(t, client.YankLine(pane))

	waitForLine(t, received, "^Y")
}

// TestIntegrationComposingRefusesWhatItCannotSend covers the arguments that would otherwise reach tmux as a
// paste of nothing or a send to whichever pane happens to be current.
func TestIntegrationComposingRefusesWhatItCannotSend(t *testing.T) {
	client := testServer(t, "refusals")

	require.Error(t, client.PasteAndSubmit("", "text"))
	require.Error(t, client.PasteAndSubmit("%0", ""))
	require.Error(t, client.PasteAndSubmit("%0", "\x1b"))
	require.Error(t, client.CutLine(""))
	require.Error(t, client.YankLine(""))
}

// rawPane is a pane whose terminal passes the control characters through rather than acting on
// them itself, alongside the file it writes what it receives to.
func rawPane(t *testing.T, client *Client, session string) (pane, received string) {
	t.Helper()

	received = filepath.Join(t.TempDir(), "received")
	script := "stty raw -echo; printf '\033[?2004h'; exec cat -uv > " + received

	pane, err := client.query("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session+":",
		"--", readlineShell, "-c", script)
	require.NoError(t, err)

	sendText(t, client, pane, "started")
	waitForLine(t, received, "started")

	return pane, received
}

// TestIntegrationCutAndYankRoundTripsThroughReadline covers what the byte tests cannot, a line editor clearing
// its own box whole and handing the text back.
func TestIntegrationCutAndYankRoundTripsThroughReadline(t *testing.T) {
	_, err := exec.LookPath(readlineShell)
	require.NoError(t, err)

	client := testServer(t, "readline")

	pane, err := client.query("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "readline:",
		"--", readlineShell, "--norc", "--noprofile", "-i")
	require.NoError(t, err)

	waitForFrame(t, client, pane, "$")
	prompt := lastLine(client.CapturePane(pane))

	sendText(t, client, pane, "echo hello world")
	waitForFrame(t, client, pane, "echo hello world")

	// The cursor is left mid-line.
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
