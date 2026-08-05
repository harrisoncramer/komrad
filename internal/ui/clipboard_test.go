package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClipboard records every value written to it, so a test can assert what a digit
// key copied without touching the real system clipboard.
type fakeClipboard struct {
	written []string
	err     error
}

func (f *fakeClipboard) WriteAll(text string) error {
	f.written = append(f.written, text)

	return f.err
}

// TestTryCopyRecordValue covers the digit key copying whichever field it names off
// the record currently on screen, and doing nothing for a key or an index the record
// does not have.
func TestTryCopyRecordValue(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		values      []string
		wantHandled bool
		wantCopied  string
	}{
		{
			name:        "the first field's key copies its value",
			key:         "1",
			values:      []string{"claude", "/repo"},
			wantHandled: true,
			wantCopied:  "claude",
		},
		{
			name:        "a later field's key copies that one instead",
			key:         "2",
			values:      []string{"claude", "/repo"},
			wantHandled: true,
			wantCopied:  "/repo",
		},
		{
			name:        "a digit past the last field is left for the rest of the switch",
			key:         "3",
			values:      []string{"claude", "/repo"},
			wantHandled: false,
		},
		{
			name:        "no record on screen leaves every digit unhandled",
			key:         "1",
			values:      nil,
			wantHandled: false,
		},
		{
			name:        "a non-digit key is never a copy",
			key:         "j",
			values:      []string{"claude"},
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clip := &fakeClipboard{}
			m := model{recordValues: tt.values, clip: clip}

			cmd, handled := m.tryCopyRecordValue(tt.key)

			require.Equal(t, tt.wantHandled, handled)
			if !tt.wantHandled {
				assert.Nil(t, cmd)
				return
			}

			require.NotNil(t, cmd)
			msg, ok := cmd().(copyResult)
			require.True(t, ok)
			assert.NoError(t, msg.err)
			assert.Equal(t, []string{tt.wantCopied}, clip.written)
		})
	}
}

// TestTryCopyRecordValueReportsAFailure covers the clipboard itself refusing the
// write, such as no clipboard utility being on the machine's PATH.
func TestTryCopyRecordValueReportsAFailure(t *testing.T) {
	clip := &fakeClipboard{err: errors.New("no clipboard utility found")}
	m := model{recordValues: []string{"claude"}, clip: clip}

	cmd, handled := m.tryCopyRecordValue("1")
	require.True(t, handled)
	require.NotNil(t, cmd)

	msg, ok := cmd().(copyResult)
	require.True(t, ok)
	assert.EqualError(t, msg.err, "no clipboard utility found")
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
