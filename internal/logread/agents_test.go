package logread

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentLine(n int, key, id string) string {
	return fmt.Sprintf(`{"timestamp":"2026-09-28T10:00:%02d.000Z","severity_text":"INFO","body":"line %d","%s":"%s"}`, n, n, key, id)
}

func appendLog(t *testing.T, path, body string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString(body)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// through is the offset just past the first n lines of a log.
func through(lines []string, n int) int64 {
	var offset int64
	for _, line := range lines[:n] {
		offset += int64(len(line)) + 1
	}

	return offset
}

// TestNewestAboutAgents covers reading the newest lines about some agents from the end of the log.
func TestNewestAboutAgents(t *testing.T) {
	t.Parallel()

	lines := []string{
		agentLine(1, "komrad.agent.id", "a1"),
		agentLine(2, "komrad.agent.id", "b2"),
		agentLine(3, "komrad.target_agent.id", "a1"),
		agentLine(4, "komrad.script.agent.id", "c3"),
		agentLine(5, "komrad.parent.id", "a1"),
		untraced,
		agentLine(6, "komrad.agent.id", "a1"),
	}

	tests := []struct {
		name   string
		ids    []string
		limit  int
		budget int
		tail   string
		want   []string
	}{
		{
			name:  "an agent the log never names",
			ids:   []string{"z9"},
			limit: 10,
		},
		{
			name:  "every key that names an agent, oldest first",
			ids:   []string{"a1"},
			limit: 10,
			want:  []string{lines[0], lines[2], lines[6]},
		},
		{
			name:  "several agents at once",
			ids:   []string{"b2", "c3"},
			limit: 10,
			want:  []string{lines[1], lines[3]},
		},
		{
			name:  "the limit keeps the newest",
			ids:   []string{"a1"},
			limit: 2,
			want:  []string{lines[2], lines[6]},
		},
		{
			name:   "the budget drops the line it starts inside",
			ids:    []string{"a1"},
			limit:  10,
			budget: len(lines[6]) + len(untraced) + 18,
			want:   []string{lines[6]},
		},
		{
			name:   "a budget starting on a line's first byte keeps that line",
			ids:    []string{"a1", "c3"},
			limit:  10,
			budget: int(through(lines, 7) - through(lines, 3)),
			want:   []string{lines[3], lines[6]},
		},
		{
			name:  "a half-written last line is left for the next read",
			ids:   []string{"a1"},
			limit: 10,
			tail:  `{"timestamp":"2026-09-28T10:00:07.000Z","komrad.agent.id":"a1"`,
			want:  []string{lines[0], lines[2], lines[6]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeLog(t, lines...)
			appendLog(t, path, tt.tail)
			reader := New(path)
			if tt.budget > 0 {
				reader.budget = tt.budget
			}

			window, err := reader.NewestAboutAgents(t.Context(), tt.ids, tt.limit)
			require.NoError(t, err)
			assert.Equal(t, tt.want, window.Lines)
			assert.Equal(t, through(lines, len(lines)), window.Offset, "the offset is the end of the last whole line")
			assert.False(t, window.More)
		})
	}
}

// TestAboutAgentsSince covers reading what was written about some agents after an offset.
func TestAboutAgentsSince(t *testing.T) {
	t.Parallel()

	short := []string{
		agentLine(1, "komrad.agent.id", "a1"),
		agentLine(2, "komrad.agent.id", "b2"),
		agentLine(3, "komrad.agent.id", "a1"),
	}
	long := strings.Replace(agentLine(4, "komrad.agent.id", "a1"), "{", `{"pad":"`+strings.Repeat("x", 200)+`",`, 1)
	withLong := []string{short[0], long, short[2]}

	tests := []struct {
		name        string
		log         []string
		tail        string
		from        int
		pastEnd     bool
		limit       int
		budget      int
		maxLine     int
		cancelled   bool
		want        []string
		wantThrough int
		wantMore    bool
		wantReset   bool
		wantErr     error
	}{
		{
			name:      "a cancelled read stops",
			log:       short,
			limit:     10,
			cancelled: true,
			wantErr:   context.Canceled,
		},
		{
			name:        "an offset past the end starts over from the newest",
			log:         short,
			pastEnd:     true,
			limit:       10,
			want:        []string{short[0], short[2]},
			wantThrough: 3,
			wantReset:   true,
		},
		{
			name:        "a line longer than the cap is skipped",
			log:         withLong,
			limit:       10,
			maxLine:     len(short[0]) + 16,
			want:        []string{short[0], short[2]},
			wantThrough: 3,
		},
		{
			name:        "nothing new leaves the offset where it was",
			log:         short,
			from:        3,
			limit:       10,
			wantThrough: 3,
		},
		{
			name:        "a half-written line is left for the next read",
			log:         short,
			tail:        short[2][:20],
			from:        1,
			limit:       10,
			want:        []string{short[2]},
			wantThrough: 3,
		},
		{
			name:        "lines about other agents are passed over",
			log:         short,
			from:        1,
			limit:       10,
			want:        []string{short[2]},
			wantThrough: 3,
		},
		{
			name:        "the limit stops the read and says more remains",
			log:         short,
			limit:       1,
			want:        short[:1],
			wantThrough: 1,
			wantMore:    true,
		},
		{
			name:        "the budget stops the read and says more remains",
			log:         short,
			limit:       10,
			budget:      len(short[0]) + 1,
			want:        short[:1],
			wantThrough: 1,
			wantMore:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeLog(t, tt.log...)
			appendLog(t, path, tt.tail)
			reader := New(path)
			if tt.budget > 0 {
				reader.budget = tt.budget
			}
			if tt.maxLine > 0 {
				reader.maxLine = tt.maxLine
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			offset := through(tt.log, tt.from)
			if tt.pastEnd {
				offset = 1 << 20
			}

			window, err := reader.AboutAgentsSince(ctx, []string{"a1"}, offset, tt.limit)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, window.Lines)
			assert.Equal(t, through(tt.log, tt.wantThrough), window.Offset)
			assert.Equal(t, tt.wantMore, window.More)
			assert.Equal(t, tt.wantReset, window.Reset)
		})
	}
}

// TestAgentLogsWithoutALog covers asking before the service has ever written anything.
func TestAgentLogsWithoutALog(t *testing.T) {
	t.Parallel()

	reader := New(filepath.Join(t.TempDir(), "missing.log"))

	_, err := reader.NewestAboutAgents(t.Context(), []string{"a1"}, 10)
	require.ErrorIs(t, err, ErrNoLog)

	_, err = reader.AboutAgentsSince(t.Context(), []string{"a1"}, 0, 10)
	require.ErrorIs(t, err, ErrNoLog)
}
