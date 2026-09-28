package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadAppendOnlyLogReadsOnlyWhatIsFinished pins what one pass over a log a CLI is still
// writing hands back, along with the offset the next pass starts from.
func TestReadAppendOnlyLogReadsOnlyWhatIsFinished(t *testing.T) {
	tests := []struct {
		name         string
		before       string
		after        string
		wantRestart  bool
		wantConsumed int64
		wantRead     string
	}{
		{
			name:  "a log that is not on disk at all, which is every pass before the CLI starts",
			after: "\x00",
		},
		{
			name:  "a log holding nothing yet",
			after: "",
		},
		{
			name:         "whole records are handed over",
			after:        "one\ntwo\n",
			wantConsumed: 8,
			wantRead:     "one\ntwo\n",
		},
		{
			name:         "a half-written record waits for its newline",
			after:        "one\ntw",
			wantConsumed: 4,
			wantRead:     "one\n",
		},
		{
			name:  "a record with no newline at all is not read yet",
			after: "not finished",
		},
		{
			name:         "only what was appended since the offset is read",
			before:       "one\n",
			after:        "one\ntwo\n",
			wantConsumed: 4,
			wantRead:     "two\n",
		},
		{
			name:   "a log that has not moved is read again as nothing",
			before: "one\n",
			after:  "one\n",
		},
		{
			name:         "a log shorter than the offset it was read at is a different log",
			before:       "a longer conversation\n",
			after:        "new\n",
			wantRestart:  true,
			wantConsumed: 4,
			wantRead:     "new\n",
		},
		{
			name:        "a log emptied between passes is a restart with nothing to read yet",
			before:      "one\n",
			after:       "",
			wantRestart: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "log.jsonl")

			var offset int64
			if tt.before != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.before), 0o600))
				offset = int64(len(tt.before))
			}
			if tt.after != "\x00" {
				require.NoError(t, os.WriteFile(path, []byte(tt.after), 0o600))
			}

			var read string
			got, err := ReadAppendOnlyLog(path, offset, func(b []byte) []Turn {
				read = string(b)

				return []Turn{{Role: "user", Content: string(b)}}
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantRestart, got.Restart)
			assert.Equal(t, tt.wantConsumed, got.Consumed)
			assert.Equal(t, tt.wantRead, read, "the bytes handed to the harness to flatten")
		})
	}
}

// TestCleanTextDropsWhatTheTranscriptColumnsCannotHold pins the NUL, which Postgres refuses
// outright rather than storing, so a turn carrying one would fail the whole pass.
func TestCleanTextDropsWhatTheTranscriptColumnsCannotHold(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "prose is trimmed",
			text: "  a turn  \n",
			want: "a turn",
		},
		{
			name: "a NUL is dropped",
			text: "before\x00after",
			want: "beforeafter",
		},
		{
			name: "a turn that is only a NUL comes back empty",
			text: "\x00",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CleanText(tt.text))
		})
	}
}
