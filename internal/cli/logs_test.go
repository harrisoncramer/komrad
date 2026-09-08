package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeLogTail covers how the log is folded into a reason: a dial failure spans several
// indented lines, and the error it becomes is rendered wrapped, so it is carried as one line
// rather than with the log's own breaks in it.
func TestServeLogTail(t *testing.T) {
	dialFailure := "failed to reach db: failed to connect:\n\t[::1]:5460: dial error: connection refused\n"

	tests := []struct {
		name     string
		contents string
		offset   int64
		want     string
	}{
		{
			name:     "an empty log has no reason to give",
			contents: "\n\n",
		},
		{
			name:     "a multi-line failure is folded onto one line",
			contents: dialFailure,
			want:     "failed to reach db: failed to connect: [::1]:5460: dial error: connection refused",
		},
		{
			name:     "reading past the end gives nothing rather than the older lines",
			contents: dialFailure,
			offset:   int64(len(dialFailure)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "serve.log")
			require.NoError(t, os.WriteFile(path, []byte(tt.contents), 0o644))

			assert.Equal(t, tt.want, serveLogTail(path, tt.offset))
		})
	}
}

// TestServeLogTailIsBounded covers that a server which logged a great deal is cut down to its
// last lines, keeping the end where the failure that stopped it is.
func TestServeLogTailIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")

	var log strings.Builder
	for i := range serveLogLines * 3 {
		fmt.Fprintf(&log, "line %d\n", i)
	}
	require.NoError(t, os.WriteFile(path, []byte(log.String()), 0o644))

	tail := serveLogTail(path, 0)

	assert.Len(t, strings.Split(tail, " "), serveLogLines*2, "each kept line is two words")
	assert.Contains(t, tail, fmt.Sprintf("line %d", serveLogLines*3-1), "the last line is kept")
	assert.NotContains(t, tail, "line 0 ", "the first is dropped")
}

// TestServeLogTailWithNoLog covers a log that could not be read at all, which is
// the case when the file could not be opened for the server to write to either.
func TestServeLogTailWithNoLog(t *testing.T) {
	assert.Empty(t, serveLogTail(filepath.Join(t.TempDir(), "absent.log"), 0))
}
