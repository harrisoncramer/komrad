package ui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// copyResultFrom runs a command returned by the copy key, which batches the clipboard write
// with the flash's revert timer, and picks the copyResult out of it.
func copyResultFrom(t *testing.T, cmd tea.Cmd) copyResult {
	t.Helper()

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok, "the copy key batches the write with the flash's revert timer")

	for _, sub := range batch {
		if msg, ok := sub().(copyResult); ok {
			return msg
		}
	}

	t.Fatal("no copyResult among the batched commands")

	return copyResult{}
}

// fakeClipboard records every value written to it, so a test can assert what the copy key
// took without touching the real system clipboard.
type fakeClipboard struct {
	written []string
	err     error
}

func (f *fakeClipboard) WriteAll(text string) error {
	f.written = append(f.written, text)

	return f.err
}

// TestCopyResultReportsOnFailureAndClearsOnSuccess covers the banner the panel shows
// when a copy is refused, and that a later success clears whatever the last one said.
func TestCopyResultReportsOnFailureAndClearsOnSuccess(t *testing.T) {
	m := model{}

	updated, cmd := m.Update(copyResult{err: errors.New("no clipboard utility found")})
	next, ok := updated.(model)
	require.True(t, ok)
	require.Nil(t, cmd)
	require.NotNil(t, next.banner)
	assert.Contains(t, next.banner.Summary, "clipboard")

	cleared, _ := next.Update(copyResult{err: nil})
	final, ok := cleared.(model)
	require.True(t, ok)
	assert.Nil(t, final.banner)
}
