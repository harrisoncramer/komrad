package pullrequests

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/forge"
	"komrad/internal/logging"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
)

// pullRequestThrottleFloor is the wait after a rate limit refusal that named no time, doubled
// on each refusal in a row from the same host up to pullRequestThrottleCeiling.
const (
	pullRequestThrottleFloor   = time.Minute
	pullRequestThrottleCeiling = time.Hour
)

// pullRequestForgeFinder is what opening and throttling a host asks of the forge service.
type pullRequestForgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
	SetThrottledUntil(ctx context.Context, id string, until *time.Time) error
}

type hostBackoff struct {
	forges pullRequestForgeFinder
	now    func() time.Time
	// refusals counts rate limit refusals in a row per host that named no time to wait.
	refusals map[string]int
}

// forgeHost is one forge host being read in a pass, with the connection and forge it is read
// through.
type forgeHost struct {
	host       string
	connection *forgestore.Connection
	forge      forge.Forge
	// throttled says nothing more is asked of the host this pass.
	throttled bool
}

func newHostBackoff(forges pullRequestForgeFinder, now func() time.Time) *hostBackoff {
	return &hostBackoff{
		forges:   forges,
		now:      now,
		refusals: map[string]int{},
	}
}

// openHost is the host with its connection and forge, nil when it cannot be read this pass.
func (b *hostBackoff) openHost(ctx context.Context, hostname string) (host *forgeHost, unconnected bool) {
	connection, err := b.forges.ConnectionFor(ctx, hostname)
	if errors.Is(err, store.ErrNotFound) {
		return nil, true
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to find the forge connection",
			append([]slog.Attr{slog.String(logging.ForgeHost, hostname)}, logging.Err(err)...)...)

		return nil, false
	}
	if connection.ThrottledUntil != nil && connection.ThrottledUntil.After(b.now()) {
		return nil, false
	}

	hostForge, err := b.forges.ForConnection(*connection)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to build the forge",
			append([]slog.Attr{slog.String(logging.ForgeHost, hostname)}, logging.Err(err)...)...)

		return nil, false
	}

	return &forgeHost{
		host:       hostname,
		connection: connection,
		forge:      hostForge,
	}, false
}

// throttle stops the host for the pass and records when to try it again, answering that time or zero when the write failed.
func (b *hostBackoff) throttle(ctx context.Context, host *forgeHost, retryAt time.Time) time.Time {
	host.throttled = true
	until := retryAt
	if until.IsZero() {
		wait := pullRequestThrottleFloor
		for range b.refusals[host.host] {
			wait = min(wait*2, pullRequestThrottleCeiling)
		}
		b.refusals[host.host]++
		until = b.now().Add(wait)
	}

	if err := b.forges.SetThrottledUntil(ctx, host.connection.ID, &until); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record the forge's rate limit", logging.Err(err)...)

		return time.Time{}
	}

	return until
}

func (b *hostBackoff) reset(host string) {
	delete(b.refusals, host)
}
