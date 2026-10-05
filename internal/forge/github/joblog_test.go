package github

import (
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jobLogPath = "/repos/harrisoncramer/komrad/actions/jobs/42/logs"

// TestJobLogFollowsTheRedirectWithoutTheCredential covers that the log is read from the signed address
// GitHub redirects to, and that only the call to GitHub carries the token.
func TestJobLogFollowsTheRedirectWithoutTheCredential(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		jobLogPath: {status: http.StatusFound, header: map[string]string{"Location": "/signed?sig=abc"}},
		"/signed":  {body: "2026-09-20T12:00:00Z go test\n"},
	})

	log, err := f.JobLog(t.Context(), komrad, 42)
	require.NoError(t, err)
	assert.Equal(t, &forge.JobLog{Text: "2026-09-20T12:00:00Z go test\n"}, log)

	require.Len(t, fake.calls, 2)
	assert.Equal(t, jobLogPath, fake.calls[0].Path)
	assert.Equal(t, "Bearer secret", fake.calls[0].Header.Get("Authorization"))
	assert.Equal(t, "/signed", fake.calls[1].Path)
	assert.Empty(t, fake.calls[1].Header.Get("Authorization"))
}

// TestJobLogRefusals covers how each answer GitHub gives in place of a redirect reads.
func TestJobLogRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		answers       map[string]answer
		wantIs        error
		wantRetryAt   time.Time
		wantThrottled bool
	}{
		{
			name:    "a log not ready yet is not found",
			answers: map[string]answer{jobLogPath: {status: http.StatusNotFound, body: `{"message":"Not Found"}`}},
			wantIs:  forge.ErrNotFound,
		},
		{
			name:    "a log GitHub no longer keeps is expired",
			answers: map[string]answer{jobLogPath: {status: http.StatusGone, body: `{"message":"Gone"}`}},
			wantIs:  forge.ErrExpired,
		},
		{
			name: "a spent rate limit is a throttle",
			answers: map[string]answer{jobLogPath: {
				status: http.StatusForbidden,
				header: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1789900000"},
				body:   `{"message":"API rate limit exceeded"}`,
			}},
			wantIs:        forge.ErrThrottled,
			wantThrottled: true,
			wantRetryAt:   time.Unix(1789900000, 0),
		},
		{
			name:    "a server error is a plain failure",
			answers: map[string]answer{jobLogPath: {status: http.StatusInternalServerError, body: `{"message":"boom"}`}},
		},
		{
			name:    "a redirect with no address is a plain failure",
			answers: map[string]answer{jobLogPath: {status: http.StatusFound}},
		},
		{
			name: "a signed address that refuses is a plain failure",
			answers: map[string]answer{
				jobLogPath: {status: http.StatusFound, header: map[string]string{"Location": "/signed"}},
				"/signed":  {status: http.StatusForbidden, body: "expired signature"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, tt.answers)

			_, err := f.JobLog(t.Context(), komrad, 42)
			require.Error(t, err)

			if tt.wantIs == nil {
				require.NotErrorIs(t, err, forge.ErrNotFound)
				require.NotErrorIs(t, err, forge.ErrThrottled)

				return
			}
			require.ErrorIs(t, err, tt.wantIs)

			if tt.wantThrottled {
				var throttled *forge.ThrottledError
				require.ErrorAs(t, err, &throttled)
				assert.True(t, throttled.RetryAt.Equal(tt.wantRetryAt), "retry at %s, want %s", throttled.RetryAt, tt.wantRetryAt)
			}
		})
	}
}

// TestJobLogTruncatesAtAWholeLine covers that a log past the most komrad keeps is cut back to its last
// whole line, so a character straddling the limit is not split.
func TestJobLogTruncatesAtAWholeLine(t *testing.T) {
	t.Parallel()

	kept := strings.Repeat("a\n", (maxJobLogBytes-1)/2)
	f, _, _ := serve(t, map[string]answer{
		jobLogPath: {status: http.StatusFound, header: map[string]string{"Location": "/signed"}},
		"/signed":  {body: kept + "aé\n"},
	})

	log, err := f.JobLog(t.Context(), komrad, 42)
	require.NoError(t, err)
	assert.True(t, log.Truncated)
	assert.Len(t, log.Text, len(kept))
	assert.True(t, utf8.ValidString(log.Text))
	assert.True(t, strings.HasSuffix(log.Text, "a\n"))
}
