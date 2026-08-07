package ui

import (
	"errors"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// copyResultFrom runs a command returned by tryCopyRecordValue, which batches the
// clipboard write with the flash's revert timer, and picks the copyResult out of it.
func copyResultFrom(t *testing.T, cmd tea.Cmd) copyResult {
	t.Helper()

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok, "tryCopyRecordValue batches the copy with the flash's revert timer")

	for _, sub := range batch {
		if msg, ok := sub().(copyResult); ok {
			return msg
		}
	}

	t.Fatal("no copyResult among the batched commands")

	return copyResult{}
}

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
// does not have. These cases are about the copy itself; the flash is covered on its own
// below.
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
			m := model{activeRecord: record{values: tt.values}, frame: stageFrame(&recordingTmux{}), clip: clip}

			cmd, handled := m.tryCopyRecordValue(tt.key)

			require.Equal(t, tt.wantHandled, handled)
			if !tt.wantHandled {
				assert.Nil(t, cmd)
				return
			}

			require.NotNil(t, cmd)
			msg := copyResultFrom(t, cmd)
			assert.NoError(t, msg.err)
			assert.Equal(t, []string{tt.wantCopied}, clip.written)
		})
	}
}

// TestTryCopyRecordValueReportsAFailure covers the clipboard itself refusing the
// write, such as no clipboard utility being on the machine's PATH.
func TestTryCopyRecordValueReportsAFailure(t *testing.T) {
	clip := &fakeClipboard{err: errors.New("no clipboard utility found")}
	m := model{activeRecord: record{values: []string{"claude"}}, frame: stageFrame(&recordingTmux{}), clip: clip}

	cmd, handled := m.tryCopyRecordValue("1")
	require.True(t, handled)
	require.NotNil(t, cmd)

	msg := copyResultFrom(t, cmd)
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

// TestTryCopyRecordValueFlashesTheFieldsLine covers the copied field's own line reversing in
// the stage, which is the only sign a digit key registered.
func TestTryCopyRecordValueFlashesTheFieldsLine(t *testing.T) {
	md := "# agent\n\n---\n\n1. agent_id: a1\n2. directory: /repo\n"

	recorder := &recordingTmux{}
	m := model{
		activeRecord: record{markdown: md, values: []string{"a1", "/repo"}},
		frame:        stageFrame(recorder),
		clip:         &fakeClipboard{},
	}

	_, handled := m.tryCopyRecordValue("2")
	require.True(t, handled)

	assert.Equal(t, []string{"%1"}, recorder.respawnPanes, "the field flashes in the pane the record is in")

	// A flash leaves the region holding nothing a plain document matches, so the
	// redraw that ends it is not skipped as showing what is already there.
	m.frame.Show(layout.RegionStage, newTextContent(md))

	assert.Len(t, recorder.respawnPanes, 2)
}

// stageFrame is a frame attached to a sidebar pane, so a record shown in the stage lands in
// a pane of its own.
func stageFrame(tmux *recordingTmux) *layout.Frame {
	frame := layout.New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	return frame
}
