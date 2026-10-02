package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
)

// TestPacingFail pins how a failed read paces the repository's next reads.
func TestPacingFail(t *testing.T) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	repository := func(fingerprint string, askedAt time.Time) candidate {
		return candidate{
			repositoryURL: "https://github.com/acme/widgets",
			remote:        git.RemoteURL{Host: "github.com"},
			fingerprint:   fingerprint,
			askedAt:       askedAt,
		}
	}

	type step struct {
		failed      bool
		fingerprint string
		at          time.Duration
	}
	tests := []struct {
		name    string
		steps   []step
		checkAt time.Duration
		wantDue bool
	}{
		{
			name: "a moved repository whose read failed is retried cold",
			steps: []step{
				{
					fingerprint: "main@a",
				},
				{
					failed:      true,
					fingerprint: "main@b",
					at:          10 * time.Second,
				},
			},
			checkAt: 10*time.Second + hotGap,
			wantDue: false,
		},
		{
			name: "a move seen by a failed read keeps the next read hot",
			steps: []step{
				{
					fingerprint: "main@a",
				},
				{
					failed:      true,
					fingerprint: "main@b",
					at:          10 * time.Second,
				},
				{
					fingerprint: "main@b",
					at:          10*time.Second + coldGap,
				},
			},
			checkAt: 10*time.Second + coldGap + hotGap,
			wantDue: true,
		},
		{
			name: "a failed read keeps a move already earned",
			steps: []step{
				{
					fingerprint: "main@a",
				},
				{
					fingerprint: "main@b",
					at:          10 * time.Second,
				},
				{
					failed:      true,
					fingerprint: "main@b",
					at:          20 * time.Second,
				},
				{
					fingerprint: "main@b",
					at:          20*time.Second + coldGap,
				},
			},
			checkAt: 20*time.Second + coldGap + hotGap,
			wantDue: true,
		},
		{
			name: "a move seen by a failed read is forgotten after movedHotWithin",
			steps: []step{
				{
					fingerprint: "main@a",
				},
				{
					failed:      true,
					fingerprint: "main@b",
					at:          10 * time.Second,
				},
				{
					fingerprint: "main@b",
					at:          10*time.Second + movedHotWithin,
				},
			},
			checkAt: 10*time.Second + movedHotWithin + hotGap,
			wantDue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pace := newPacing()
			for _, next := range tt.steps {
				if next.failed {
					pace.fail(repository(next.fingerprint, start.Add(next.at)))

					continue
				}
				pace.read(repository(next.fingerprint, start.Add(next.at)), time.Time{})
			}

			last := tt.steps[len(tt.steps)-1]
			due := pace.pick([]candidate{repository(last.fingerprint, time.Time{})}, start.Add(tt.checkAt))

			assert.Equal(t, tt.wantDue, len(due) == 1)
		})
	}
}
