package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPagerArgv covers the command a pane runs, which carries the line to open on for a
// search preview and nothing for a record read from the top.
func TestPagerArgv(t *testing.T) {
	tests := []struct {
		name string
		line int
		want []string
	}{
		{
			name: "no line opens at the top",
			line: 0,
			want: []string{"less", "-R", "-X", "-Ps", "/tmp/stage"},
		},
		{
			name: "the first line is the top already",
			line: 1,
			want: []string{"less", "-R", "-X", "-Ps", "/tmp/stage"},
		},
		{
			name: "a line further down is where the pager opens",
			line: 42,
			want: []string{"less", "-R", "-X", "-Ps", "+42", "/tmp/stage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pagerArgv("/tmp/stage", tt.line))
		})
	}
}

// TestStageFilePathIsThisPanelsOwn covers the name a screenful is written under, which carries
// the pid.
func TestStageFilePathIsThisPanelsOwn(t *testing.T) {
	path := stageFilePath("review")

	assert.Contains(t, filepath.Base(path), strconv.Itoa(os.Getpid()), "the panel that wrote it")
	assert.Equal(t, filepath.Join(os.TempDir(), filepath.Base(path)), path) //nolint:usetesting // the shared temporary directory is what is being asserted, so a per-test one would assert nothing
	assert.NotEqual(t, path, stageFilePath("transcript"), "two regions never share a file")
}

// TestWriteStageFileIsNeverTornInHalf covers the handover to the drawer, which reads the file
// whenever the panel taps a key and so can read one frame while the next is being written.
func TestWriteStageFileIsNeverTornInHalf(t *testing.T) {
	const frames = 200

	path := filepath.Join(t.TempDir(), "stage.ansi")
	tall := strings.Repeat("a tall frame\n", 400)
	short := "one row\n"

	require.NoError(t, writeStageFile(path, tall))

	written := 0
	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range frames {
			frame := tall
			if i%2 == 0 {
				frame = short
			}

			if writeStageFile(path, frame) != nil {
				return
			}

			written++
		}
	}()

	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}

		body, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, []string{tall, short}, string(body), "a frame was read half written")
	}

	assert.Equal(t, frames, written, "every frame was written")
}

// TestWriteStageFile covers two panels writing the same stage file and a write that cannot land.
func TestWriteStageFile(t *testing.T) {
	t.Run("two writers at once each land a whole frame", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "stage.ansi")
		frames := []string{
			strings.Repeat("first\n", 400),
			strings.Repeat("second\n", 400),
		}

		errs := make(chan error, len(frames)*50)
		var group sync.WaitGroup
		for _, frame := range frames {
			group.Go(func() {
				for range 50 {
					errs <- writeStageFile(path, frame)
				}
			})
		}
		group.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		body, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, frames, string(body))
	})

	t.Run("a write that cannot be moved into place leaves no temporary file behind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "stage.ansi")
		require.NoError(t, os.MkdirAll(filepath.Join(path, "occupied"), 0o750))

		require.Error(t, writeStageFile(path, "one row\n"))

		left, err := filepath.Glob(filepath.Join(dir, "stage.ansi.*.tmp"))
		require.NoError(t, err)
		assert.Empty(t, left)
	})
}
