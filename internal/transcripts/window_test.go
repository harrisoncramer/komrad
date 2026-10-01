package transcripts

import (
	"strings"
	"testing"

	"komrad/internal/fail"

	transcriptstore "komrad/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCutWindow covers the cut a cursor is named from, which has to move forward by whole
// characters and stop rather than name a cursor asking the same question again.
func TestCutWindow(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		offset     int64
		requested  int32
		want       string
		wantOffset int64
		wantLast   bool
		wantErr    string
	}{
		{
			name:      "a window too small to hold the next character is refused",
			content:   "日本語",
			requested: 2,
			wantErr:   "max_bytes of 2 is too small",
		},
		{
			name:    "a cursor landing inside a character says so rather than blaming the window",
			content: "日本語 and then a long tail of ordinary text that follows on",
			offset:  1,
			wantErr: "cursor no longer names the start of a character",
		},
		{
			name:     "content inside the window is the last page",
			content:  "the whole conversation",
			want:     "the whole conversation",
			wantLast: true,
		},
		{
			name:       "content past the window is cut and carries a cursor",
			content:    "opening question and then some",
			requested:  7,
			want:       "opening",
			wantOffset: 7,
		},
		{
			name:       "a cut inside a character pulls back to the one before it",
			content:    "日本語",
			requested:  7,
			want:       "日本",
			wantOffset: 6,
		},
		{
			name:     "an offset the caller was handed reads on from there",
			content:  "opening question",
			offset:   8,
			want:     "question",
			wantLast: true,
		},
		{
			name:     "an offset at the end is the empty last page",
			content:  "opening",
			offset:   7,
			want:     "",
			wantLast: true,
		},
		{
			name:     "an offset past the end is the empty last page too",
			content:  "opening",
			offset:   500,
			want:     "",
			wantLast: true,
		},
		{
			name:       "a window bigger than the cap is capped",
			content:    strings.Repeat("a", maxWindow+10),
			want:       strings.Repeat("a", maxWindow),
			wantOffset: maxWindow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cursor *transcriptstore.ContentCursor
			if tt.offset != 0 {
				cursor = &transcriptstore.ContentCursor{Offset: tt.offset}
			}

			window, err := cutWindow(tt.content, cursor, tt.requested)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.NotNil(t, fail.Of(err))
				assert.Equal(t, fail.KindInvalidArgument, fail.Of(err).Kind())
				assert.Contains(t, err.Error(), tt.wantErr, "the message has to name the real cause")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, window.Content)

			if tt.wantLast {
				assert.Nil(t, window.Next, "the last page names no cursor")

				return
			}

			require.NotNil(t, window.Next)
			assert.Equal(t, tt.wantOffset, window.Next.Offset)
		})
	}
}

// TestCutWindowPagesToTheEnd pins that the windows join back into the transcript that was stored.
func TestCutWindowPagesToTheEnd(t *testing.T) {
	whole := strings.Repeat("a", 50) + strings.Repeat("b", 50) + strings.Repeat("c", 50)

	var (
		read   strings.Builder
		cursor *transcriptstore.ContentCursor
		pages  int
	)

	for {
		window, err := cutWindow(whole, cursor, 40)
		require.NoError(t, err)

		read.WriteString(window.Content)
		pages++
		require.Less(t, pages, 20, "the windows have to run out")

		cursor = window.Next
		if cursor == nil {
			break
		}
	}

	assert.Equal(t, whole, read.String())
	assert.Equal(t, 4, pages)
}
