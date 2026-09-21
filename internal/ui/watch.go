package ui

import (
	"context"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"time"

	"weave/internal/client"
	"weave/internal/logging"
	"weave/internal/ui/panel"
	"weave/proto/gen/watch/v1/watchv1connect"
)

const (
	// pollInterval is how often the tick lists everything while the watch is delivering.
	pollInterval = 15 * time.Second
	// driftLogTimeout bounds the drift log's own context.
	driftLogTimeout = time.Second
)

// watchLostMsg says the stream has ended and the tick is the only source until it is back.
type watchLostMsg struct {
	err error
}

// watchListings holds the watch open for the life of ctx, posting each listing it builds as the
// message the poll's own listing arrives as.
func watchListings(ctx context.Context, watches watchv1connect.WatchServiceClient, post *postbox) {
	client.Watch(ctx, client.WatchParams{
		Open:      client.OpenWatch(watches),
		OnListing: func(listing client.WatchListing) { post.send(watchedResultFrom(listing)) },
		OnLost: func(err error) {
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "the watch stream ended", logging.Err(err)...)

			post.send(watchLostMsg{err: err})
		},
	})
}

// watchedResultFrom is a listing the watch built as the message the poll produces.
func watchedResultFrom(listing client.WatchListing) refreshResult {
	return refreshResult{
		watched:       true,
		agents:        agentItemsFrom(listing.Agents),
		agentMarks:    marksFromAgents(listing.Agents),
		workflows:     workflowsFrom(listing.Workflows),
		workflowMarks: marksFromWorkflows(listing.Workflows),
		schedules:     scheduleItemsFrom(listing.Schedules),
	}
}

// isPollDue says whether the tick lists everything: always while the watch is not delivering
// or the screen draws a listing the watch does not carry, and every pollInterval otherwise.
func (m model) isPollDue(now time.Time) bool {
	needs := m.active().needs()
	if !m.watching || needs.archive || needs.worktrees {
		return true
	}

	return m.polledAt.IsZero() || now.Sub(m.polledAt) >= pollInterval
}

const (
	driftChanged     = "changed"
	driftOnlyPolled  = "only_polled"
	driftOnlyWatched = "only_watched"
)

// logDrift says at debug level where a poll disagrees with the listing the watch has built.
func logDrift(watched listing, polled refreshResult) {
	ctx, cancel := context.WithTimeout(context.Background(), driftLogTimeout)
	defer cancel()

	logger := logging.From(ctx)

	report := func(how string, row slog.Attr) {
		logger.LogAttrs(ctx, slog.LevelDebug, "the poll disagrees with the watch", slog.String(logging.WatchDrift, how), row)
	}

	if polled.agentsErr == nil {
		compareRows(watched.agents, polled.agents, func(item agentItem) string { return item.agent.ID }, func(how, id string) {
			report(how, slog.String(logging.AgentID, id))
		})
	}
	if polled.workflowsErr == nil {
		compareRows(watched.workflows, polled.workflows, func(item panel.Workflow) string { return item.ID }, func(how, id string) {
			report(how, slog.String(logging.WorkflowID, id))
		})
	}
	if polled.schedulesErr == nil {
		compareRows(watched.schedules, polled.schedules, func(item scheduleItem) string { return item.schedule.ID }, func(how, id string) {
			report(how, slog.String(logging.ScheduleID, id))
		})
	}
	if polled.agentsErr != nil || polled.workflowsErr != nil {
		return
	}

	marks := make(map[string]string, len(polled.agentMarks)+len(polled.workflowMarks))
	maps.Copy(marks, polled.agentMarks)
	maps.Copy(marks, polled.workflowMarks)

	keys := slices.Sorted(maps.Keys(marks))
	for _, key := range slices.Sorted(maps.Keys(watched.marks)) {
		if _, ok := marks[key]; !ok {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		if watched.marks[key] != marks[key] {
			report(driftChanged, slog.String(logging.RowKey, key))
		}
	}
}

// compareRows reports each id the two listings disagree on.
func compareRows[T any](watched, polled []T, idOf func(T) string, report func(how, id string)) {
	held := make(map[string]T, len(watched))
	for _, row := range watched {
		held[idOf(row)] = row
	}

	seen := make(map[string]bool, len(polled))
	for _, row := range polled {
		id := idOf(row)
		seen[id] = true

		was, ok := held[id]
		switch {
		case !ok:
			report(driftOnlyPolled, id)
		case !reflect.DeepEqual(was, row):
			report(driftChanged, id)
		}
	}

	for _, row := range watched {
		if id := idOf(row); !seen[id] {
			report(driftOnlyWatched, id)
		}
	}
}
