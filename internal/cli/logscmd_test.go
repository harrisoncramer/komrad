package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// record is one line of the log, written the way the service writes it.
func record(message string) string {
	return `{"timestamp":"2026-08-02T14:00:00Z","severity_text":"INFO","body":"` + message + `"}` + "\n"
}

// safeBuffer is a buffer written by the follow and read by the test while the follow is
// still going, which without the lock is a race the detector fails the test over.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Write(p) //nolint:wrapcheck // a buffer's own error, which it never returns
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.String()
}

// TestWriteLogEndsAtTheLastRecord covers the ordinary read, which is the whole file and
// then the end of it. Nothing waits, since a command that did could not be piped into
// anything that expects an end.
func TestWriteLogEndsAtTheLastRecord(t *testing.T) {
	in := strings.NewReader(record("first") + record("second"))

	var out bytes.Buffer
	require.NoError(t, writeLog(t.Context(), &out, in, logOutput{short: true}))

	assert.Contains(t, out.String(), "first")
	assert.Contains(t, out.String(), "second")
}

// TestWriteLogFollows covers the read the panel's log key opens. The end of the file means
// nothing more has been written yet, so a record appended after the read caught up is
// written too, and it is flushed rather than held back for a buffer that may never fill.
func TestWriteLogFollows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weave.log")
	require.NoError(t, os.WriteFile(path, []byte(record("before")), 0o644))

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	out := &safeBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- writeLog(ctx, out, file, logOutput{short: true, follow: true, wait: time.Millisecond})
	}()

	assert.Eventually(t, func() bool {
		return strings.Contains(out.String(), "before")
	}, time.Second, 5*time.Millisecond, "the records already in the file are written")

	appended, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	_, err = io.WriteString(appended, record("after"))
	require.NoError(t, err)
	require.NoError(t, appended.Close())

	assert.Eventually(t, func() bool {
		return strings.Contains(out.String(), "after")
	}, time.Second, 5*time.Millisecond, "and a record appended while the follow is open")

	// The follow ends where it was cancelled rather than reporting the cancellation as a
	// failure, since it was ended rather than broken.
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("the follow did not end when its context was cancelled")
	}
}
