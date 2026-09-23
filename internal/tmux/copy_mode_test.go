//go:build integration

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

// TestIntegrationPaneInAModeTakesNoKeys proves that nothing sent to a pane in a mode arrives, literally or
// not.
func TestIntegrationPaneInAModeTakesNoKeys(t *testing.T) {
	client := testServer(t, "weave")

	pane, err := client.query("display-message", "-p", "-t", "weave:", "#{pane_id}")
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

		time.Sleep(50 * time.Millisecond)
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
