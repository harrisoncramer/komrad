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

type hostGate struct {
	forges *forge.Service
	now    func() time.Time
	// refusals counts rate limit refusals in a row per host that named no time to wait.
	refusals map[string]int
	said     onceLog
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

func newHostGate(forges *forge.Service, now func() time.Time) *hostGate {
	return &hostGate{
		forges:   forges,
		now:      now,
		refusals: map[string]int{},
		said:     onceLog{},
	}
}

// open is the host with its connection and forge, nil when it cannot be read this pass.
func (g *hostGate) open(ctx context.Context, hostname string) (host *forgeHost, unconnected bool) {
	connection, err := g.forges.ConnectionFor(ctx, hostname)
	if errors.Is(err, store.ErrNotFound) {
		return nil, true
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to find the forge connection",
			append([]slog.Attr{slog.String(logging.ForgeHost, hostname)}, logging.Err(err)...)...)

		return nil, false
	}
	if connection.ThrottledUntil != nil && connection.ThrottledUntil.After(g.now()) {
		return nil, false
	}

	hostForge, err := g.forges.ForConnection(*connection)
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

// settle stops the host for the pass when the error says its rate limit or its credential did,
// and says whether it was one of those.
func (g *hostGate) settle(ctx context.Context, host *forgeHost, err error) bool {
	var throttled *forge.ThrottledError
	switch {
	case errors.As(err, &throttled):
		g.throttle(ctx, host, throttled.RetryAt)
	case errors.Is(err, forge.ErrUnauthorized):
		g.refuse(ctx, host, err)
	default:
		return false
	}

	return true
}

// throttle stops a host for the pass after a rate limit refusal, records when to try it again,
// and says so.
func (g *hostGate) throttle(ctx context.Context, host *forgeHost, retryAt time.Time) {
	until := g.backOff(ctx, host, retryAt)
	if until.IsZero() {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge is rate limiting the connection",
		slog.String(logging.ForgeHost, host.host),
		slog.Time(logging.ThrottledUntil, until))
}

// refuse stops a host for the pass after the forge refused its credential, backs off the way a
// rate limit refusal that named no time does, and says so once per host.
func (g *hostGate) refuse(ctx context.Context, host *forgeHost, err error) {
	g.backOff(ctx, host, time.Time{})
	if !g.said.first(saidRefused, host.host) {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge refused the connection's credential, so the host is not read until it is accepted again",
		append([]slog.Attr{slog.String(logging.ForgeHost, host.host)}, logging.Err(err)...)...)
}

// backOff stops the host for the pass and records when to try it again, answering that time or zero when the write failed.
func (g *hostGate) backOff(ctx context.Context, host *forgeHost, retryAt time.Time) time.Time {
	host.throttled = true
	until := retryAt
	if until.IsZero() {
		wait := pullRequestThrottleFloor
		for range g.refusals[host.host] {
			wait = min(wait*2, pullRequestThrottleCeiling)
		}
		g.refusals[host.host]++
		until = g.now().Add(wait)
	}

	if err := g.forges.SetThrottledUntil(ctx, host.connection.ID, &until); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record the forge's rate limit", logging.Err(err)...)

		return time.Time{}
	}

	return until
}

// release lets a refusal be said again once the host got through a pass unstopped.
func (g *hostGate) release(host *forgeHost) {
	if !host.throttled {
		g.said.forget(saidRefused, host.host)
	}
}

func (g *hostGate) reset(host string) {
	delete(g.refusals, host)
}
