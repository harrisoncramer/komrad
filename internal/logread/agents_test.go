package logread

import (
	"fmt"
	"os"
	"path/filepath"
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

// TestNewestAboutAgents covers reading the newest lines about some agents back from the end of the log.
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
		chunk  int
		budget int
		want   []string
	}{
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
			name:  "lines cut across chunks are read whole",
			ids:   []string{"a1", "b2", "c3"},
			limit: 10,
			chunk: 7,
			want:  []string{lines[0], lines[1], lines[2], lines[3], lines[6]},
		},
		{
			name:   "the budget stops the read before the oldest",
			ids:    []string{"a1"},
			limit:  10,
			chunk:  16,
			budget: len(lines[6]) + len(untraced) + 18,
			want:   []string{lines[6]},
		},
		{
			name:  "an agent the log never names",
			ids:   []string{"z9"},
			limit: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeLog(t, lines...)
			reader := New(path)
			if tt.chunk > 0 {
				reader.chunk = tt.chunk
			}
			if tt.budget > 0 {
				reader.budget = tt.budget
			}

			window, err := reader.NewestAboutAgents(tt.ids, tt.limit)
			require.NoError(t, err)
			assert.Equal(t, tt.want, window.Lines)

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, info.Size(), window.Offset, "the offset is the end of the last whole line")
		})
	}
}

// TestNewestAboutAgentsLeavesAHalfWrittenLine covers the service still writing the last line when it is read.
func TestNewestAboutAgentsLeavesAHalfWrittenLine(t *testing.T) {
	t.Parallel()

	whole := agentLine(1, "komrad.agent.id", "a1")
	half := `{"timestamp":"2026-09-28T10:00:02.000Z","komrad.agent.id":"a1"`
	path := writeLog(t, whole)
	appendLog(t, path, half)

	reader := New(path)
	reader.chunk = 16

	window, err := reader.NewestAboutAgents([]string{"a1"}, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{whole}, window.Lines)
	assert.Equal(t, int64(len(whole)+1), window.Offset)
}

// TestAboutAgentsSince covers reading what was written after an offset, a line at a time as it is finished.
func TestAboutAgentsSince(t *testing.T) {
	t.Parallel()

	first := agentLine(1, "komrad.agent.id", "a1")
	path := writeLog(t, first)
	reader := New(path)

	start, err := reader.NewestAboutAgents([]string{"a1"}, 10)
	require.NoError(t, err)

	nothing, err := reader.AboutAgentsSince([]string{"a1"}, start.Offset, 10)
	require.NoError(t, err)
	assert.Empty(t, nothing.Lines)
	assert.Equal(t, start.Offset, nothing.Offset, "nothing new leaves the offset where it was")

	other := agentLine(2, "komrad.agent.id", "b2")
	second := agentLine(3, "komrad.agent.id", "a1")
	appendLog(t, path, other+"\n"+second[:20])

	half, err := reader.AboutAgentsSince([]string{"a1"}, start.Offset, 10)
	require.NoError(t, err)
	assert.Empty(t, half.Lines)
	assert.Equal(t, start.Offset+int64(len(other)+1), half.Offset, "a half-written line is left for the next read")

	appendLog(t, path, second[20:]+"\n")

	done, err := reader.AboutAgentsSince([]string{"a1"}, half.Offset, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{second}, done.Lines)
	assert.False(t, done.Reset)
}

// TestAboutAgentsSinceStopsAtTheLimit covers a backlog longer than one read, which the next read picks up.
func TestAboutAgentsSinceStopsAtTheLimit(t *testing.T) {
	t.Parallel()

	lines := []string{
		agentLine(1, "komrad.agent.id", "a1"),
		agentLine(2, "komrad.agent.id", "a1"),
		agentLine(3, "komrad.agent.id", "a1"),
	}
	reader := New(writeLog(t, lines...))

	head, err := reader.AboutAgentsSince([]string{"a1"}, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, lines[:2], head.Lines)
	assert.True(t, head.More)

	rest, err := reader.AboutAgentsSince([]string{"a1"}, head.Offset, 2)
	require.NoError(t, err)
	assert.Equal(t, lines[2:], rest.Lines)
	assert.False(t, rest.More)
}

// TestAboutAgentsSinceAfterATruncation covers the log starting over under a caller holding an offset.
func TestAboutAgentsSinceAfterATruncation(t *testing.T) {
	t.Parallel()

	line := agentLine(1, "komrad.agent.id", "a1")
	reader := New(writeLog(t, line))

	window, err := reader.AboutAgentsSince([]string{"a1"}, 1<<20, 10)
	require.NoError(t, err)
	assert.True(t, window.Reset)
	assert.Equal(t, []string{line}, window.Lines)
	assert.Equal(t, int64(len(line)+1), window.Offset)
}

// TestAboutAgentsSinceReadsPastOneChunk covers a backlog, and a line, longer than one read of the file.
func TestAboutAgentsSinceReadsPastOneChunk(t *testing.T) {
	t.Parallel()

	lines := []string{
		agentLine(1, "komrad.agent.id", "a1"),
		agentLine(2, "komrad.agent.id", "b2"),
		agentLine(3, "komrad.agent.id", "a1"),
	}
	path := writeLog(t, lines...)
	reader := New(path)
	reader.chunk = 8

	window, err := reader.AboutAgentsSince([]string{"a1"}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{lines[0], lines[2]}, window.Lines)
	assert.False(t, window.More)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, info.Size(), window.Offset)
}

// TestAboutAgentsSinceStopsAtTheBudget covers a backlog longer than one read may look through.
func TestAboutAgentsSinceStopsAtTheBudget(t *testing.T) {
	t.Parallel()

	lines := []string{
		agentLine(1, "komrad.agent.id", "a1"),
		agentLine(2, "komrad.agent.id", "a1"),
	}
	reader := New(writeLog(t, lines...))
	reader.budget = len(lines[0]) + 1

	window, err := reader.AboutAgentsSince([]string{"a1"}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, lines[:1], window.Lines)
	assert.True(t, window.More)
	assert.Equal(t, int64(len(lines[0])+1), window.Offset)
}

// TestAgentLogsWithoutALog covers asking before the service has ever written anything.
func TestAgentLogsWithoutALog(t *testing.T) {
	t.Parallel()

	reader := New(filepath.Join(t.TempDir(), "missing.log"))

	_, err := reader.NewestAboutAgents([]string{"a1"}, 10)
	require.ErrorIs(t, err, ErrNoLog)

	_, err = reader.AboutAgentsSince([]string{"a1"}, 0, 10)
	require.ErrorIs(t, err, ErrNoLog)
}
