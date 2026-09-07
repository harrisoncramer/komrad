package cli

import (
	"testing"

	"weave/internal/harness"
	"weave/internal/harnesstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubPanes struct {
	frame string
}

func (s stubPanes) CapturePane(string) string { return s.frame }

// TestDoesFrameShowHarness covers a pane still at its shell reading as a harness that has yet to
// draw, and a harness weave cannot look up being taken at its word instead.
func TestDoesFrameShowHarness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		harnessID string
		frame     string
		want      bool
	}{
		{
			name:      "a harness nothing implements is taken at its word",
			harnessID: "gemini",
			frame:     "$ ",
			want:      true,
		},
		{
			name:      "a frame the harness reads as none of its own has yet to draw",
			harnessID: harnesstest.ID,
			frame:     "$ claude --resume",
			want:      false,
		},
		{
			name:      "a frame the harness is working in has drawn",
			harnessID: harnesstest.ID,
			frame:     "fake:working",
			want:      true,
		},
		{
			name:      "a frame the harness is idle in has drawn",
			harnessID: harnesstest.ID,
			frame:     "fake:idle",
			want:      true,
		},
	}

	fake, err := harnesstest.New(t.TempDir())
	require.NoError(t, err)

	registry, err := harness.NewRegistry(fake)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := paneFrameReader{
				panes:     stubPanes{frame: tt.frame},
				harnesses: registry,
			}

			assert.Equal(t, tt.want, reader.DoesFrameShowHarness("%1", tt.harnessID))
		})
	}
}
