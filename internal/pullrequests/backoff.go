package pullrequests

import (
	"context"
	"log/slog"
	"time"

	"komrad/internal/logging"
)

// pullRequestThrottleFloor is the wait after a rate limit refusal that named no time, doubled
// on each refusal in a row from the same host up to pullRequestThrottleCeiling.
const (
	pullRequestThrottleFloor   = time.Minute
	pullRequestThrottleCeiling = time.Hour
)

type throttleSetter interface {
	SetThrottledUntil(ctx context.Context, id string, until *time.Time) error
}

type hostBackoff struct {
	forges throttleSetter
	now    func() time.Time
	// refusals counts rate limit refusals in a row per host that named no time to wait.
	refusals map[string]int
}

func newHostBackoff(forges throttleSetter, now func() time.Time) *hostBackoff {
	return &hostBackoff{
		forges:   forges,
		now:      now,
		refusals: map[string]int{},
	}
}

// record writes when to try the host again, which is what the forge said or, when it said
// nothing, a wait that doubles with every refusal in a row, and answers it, zero when the
// write failed.
func (b *hostBackoff) record(ctx context.Context, host, connectionID string, retryAt time.Time) time.Time {
	until := retryAt
	if until.IsZero() {
		wait := pullRequestThrottleFloor
		for range b.refusals[host] {
			wait = min(wait*2, pullRequestThrottleCeiling)
		}
		b.refusals[host]++
		until = b.now().Add(wait)
	}

	if err := b.forges.SetThrottledUntil(ctx, connectionID, &until); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record the forge's rate limit", logging.Err(err)...)

		return time.Time{}
	}

	return until
}

func (b *hostBackoff) reset(host string) {
	delete(b.refusals, host)
}
