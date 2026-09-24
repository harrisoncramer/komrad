package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

type fakeClipboard struct {
	written []string
	err     error
}

func (f *fakeClipboard) WriteAll(text string) error {
	f.written = append(f.written, text)

	return f.err
}

// TestCopyResultReportsOnFailure covers the message the panel shows when a copy is refused, and
// that a later success says nothing rather than taking the refusal away unread.
func TestCopyResultReportsOnFailure(t *testing.T) {
	m := model{}

	updated, cmd := m.Update(copyResult{err: errors.New("no clipboard utility found")})
	next, ok := updated.(model)
	require.True(t, ok)
	require.NotNil(t, cmd, "the message that just landed blinks")
	require.NotEmpty(t, next.messages.items)
	assert.Contains(t, newestMessage(next).summary, "clipboard")

	succeeded, _ := next.Update(copyResult{err: nil})
	final, ok := succeeded.(model)
	require.True(t, ok)
	assert.Len(t, final.messages.items, 1, "a copy that worked reports nothing of its own")
}
