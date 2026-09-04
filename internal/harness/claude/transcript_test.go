package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlattenTranscript pins which of claude's log records become turns and which are
// dropped, since the shape of a record varies by what it holds.
func TestFlattenTranscript(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []harness.Turn
	}{
		{
			name: "string user prompt is kept",
			in:   `{"type":"user","message":{"content":"hello there"}}`,
			want: []harness.Turn{{Role: "user", Content: "hello there"}},
		},
		{
			name: "assistant text blocks are kept, thinking and tool_use dropped",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"thinking","thinking":"secret reasoning"},` +
				`{"type":"text","text":"visible answer"},` +
				`{"type":"tool_use","name":"Bash","input":{}}` +
				`]}}`,
			want: []harness.Turn{{Role: "assistant", Content: "visible answer"}},
		},
		{
			name: "user tool_result blocks are dropped",
			in:   `{"type":"user","message":{"content":[{"type":"tool_result","content":"exit 0"}]}}`,
			want: nil,
		},
		{
			name: "meta lines are skipped",
			in:   `{"type":"user","isMeta":true,"message":{"content":"injected context"}}`,
			want: nil,
		},
		{
			name: "non-conversation types are skipped",
			in: `{"type":"mode","mode":"normal"}` + "\n" +
				`{"type":"file-history-snapshot"}` + "\n" +
				`{"type":"system","content":"hook fired"}`,
			want: nil,
		},
		{
			name: "a NUL is dropped from the text it arrived in",
			in:   `{"type":"user","message":{"content":"before\u0000after"}}`,
			want: []harness.Turn{{Role: "user", Content: "beforeafter"}},
		},
		{
			name: "and from a text block",
			in:   `{"type":"assistant","message":{"content":[{"type":"text","text":"a\u0000b"}]}}`,
			want: []harness.Turn{{Role: "assistant", Content: "ab"}},
		},
		{
			name: "malformed lines are ignored",
			in:   "not json\n" + `{"type":"user","message":{"content":"survives"}}`,
			want: []harness.Turn{{Role: "user", Content: "survives"}},
		},
		{
			name: "turns come back in order, each tagged with who spoke",
			in: `{"type":"user","message":{"content":"question"}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"text","text":"answer"}]}}`,
			want: []harness.Turn{
				{Role: "user", Content: "question"},
				{Role: "assistant", Content: "answer"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns, _ := flattenTranscript([]byte(tt.in))
			assert.Equal(t, tt.want, turns)
		})
	}
}

// TestReadLog covers what one pass over a log reports, which is what the syncer's
// offset bookkeeping is built on: how far it may advance, and whether what is
// already stored still describes the file.
func TestReadLog(t *testing.T) {
	const firstTurn = `{"type":"user","message":{"content":"first turn"}}` + "\n"

	tests := []struct {
		name         string
		body         string
		offset       int64
		missing      bool
		wantTurns    []harness.Turn
		wantConsumed int64
		wantRestart  bool
	}{
		{
			name:    "a log that is not on disk yet reports nothing",
			missing: true,
		},
		{
			name:         "a first pass reads the whole log",
			body:         firstTurn,
			wantTurns:    []harness.Turn{{Role: "user", Content: "first turn"}},
			wantConsumed: int64(len(firstTurn)),
		},
		{
			name:   "a log that has not moved consumes nothing",
			body:   firstTurn,
			offset: int64(len(firstTurn)),
		},
		{
			name:         "only what was appended is read",
			body:         firstTurn + `{"type":"user","message":{"content":"second turn"}}` + "\n",
			offset:       int64(len(firstTurn)),
			wantTurns:    []harness.Turn{{Role: "user", Content: "second turn"}},
			wantConsumed: int64(len(`{"type":"user","message":{"content":"second turn"}}` + "\n")),
		},
		{
			name:         "a half-written line waits for its newline",
			body:         firstTurn + `{"type":"user","message":{"conte`,
			wantTurns:    []harness.Turn{{Role: "user", Content: "first turn"}},
			wantConsumed: int64(len(firstTurn)),
		},
		{
			name:         "a log that shrank asks for a restart",
			body:         firstTurn,
			offset:       9000,
			wantTurns:    []harness.Turn{{Role: "user", Content: "first turn"}},
			wantConsumed: int64(len(firstTurn)),
			wantRestart:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sess.jsonl")
			if !tt.missing {
				require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))
			}

			read, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadLog(path, tt.offset)
			require.NoError(t, err)

			assert.Equal(t, tt.wantTurns, read.Turns)
			assert.Equal(t, tt.wantConsumed, read.Consumed)
			assert.Equal(t, tt.wantRestart, read.Restart)
		})
	}
}

// TestFlattenTranscriptUsage runs the parse over a real excerpt, since Claude repeats the
// whole usage object on every content block of a reply and a hand-written sample would not.
func TestFlattenTranscriptUsage(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "usage_reported_per_content_block.jsonl"))
	require.NoError(t, err)

	_, usage := flattenTranscript(body)

	assert.Equal(t, []harness.Usage{
		{
			Key:        "msg_011Cej76GRM49VwfvbF72CQK",
			At:         time.Date(2026, 9, 4, 21, 25, 49, 12_000_000, time.UTC),
			Input:      2,
			CacheRead:  38267,
			CacheWrite: 25559,
			Output:     279,
		},
		{
			Key:        "msg_011Cej76bJepGMYYdU2taVqu",
			At:         time.Date(2026, 9, 4, 21, 25, 53, 558_000_000, time.UTC),
			Input:      2,
			CacheRead:  63826,
			CacheWrite: 11758,
			Output:     315,
		},
		{
			Key:        "msg_011Cej76wBjTsMRCBpGCMTXa",
			At:         time.Date(2026, 9, 4, 21, 25, 58, 96_000_000, time.UTC),
			Input:      2,
			CacheRead:  75584,
			CacheWrite: 423,
			Output:     232,
		},
	}, usage, "each reply is counted once even though it is written a line per block")
}

// TestFlattenTranscriptUsageSkipsWhatItCannotKey pins the lines that carry no usage weave can
// place, since a reply counted under the wrong key would be counted twice.
func TestFlattenTranscriptUsageSkipsWhatItCannotKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{
			name: "a line with no usage at all",
			in:   `{"type":"assistant","timestamp":"2026-09-04T21:25:49.012Z","message":{"id":"msg_1","content":[]}}`,
		},
		{
			name: "a line with no message id",
			in: `{"type":"assistant","timestamp":"2026-09-04T21:25:49.012Z",` +
				`"message":{"content":[],"usage":{"output_tokens":9}}}`,
		},
		{
			name: "a line with no timestamp",
			in:   `{"type":"assistant","message":{"id":"msg_1","content":[],"usage":{"output_tokens":9}}}`,
		},
		{
			name: "a line whose timestamp is not a time",
			in: `{"type":"assistant","timestamp":"yesterday",` +
				`"message":{"id":"msg_1","content":[],"usage":{"output_tokens":9}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, usage := flattenTranscript([]byte(tt.in))
			assert.Empty(t, usage)
		})
	}
}

// TestGetLogPath covers finding a conversation by globbing its session id, so a log lands
// whichever folder it was written in.
func TestGetLogPath(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-some-project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte("{}\n"), 0o600))

	h := NewHarness(NewHarnessParams{ProjectsDir: projects})
	assert.Equal(t, filepath.Join(dir, "sess-1.jsonl"), h.GetLogPath("sess-1"))
	assert.Empty(t, h.GetLogPath("sess-2"), "a session with no log on disk has no path")
	assert.Empty(t, h.GetLogPath(""), "and neither does an agent with no session")
}
