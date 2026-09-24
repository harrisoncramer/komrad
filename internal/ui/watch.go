package ui

import (
	"context"
	"log/slog"
	"time"

	"komrade/internal/assemble"
	"komrade/internal/client"
	"komrade/internal/logging"
	watchv1 "komrade/proto/gen/watch/v1"
	"komrade/proto/gen/watch/v1/watchv1connect"

	tea "charm.land/bubbletea/v2"
)

// watchLostMsg says the stream has ended and is being opened again.
type watchLostMsg struct {
	err error
}

// watchRows holds the watch open for the life of ctx, posting each batch it delivers.
func watchRows(ctx context.Context, watches watchv1connect.WatchServiceClient, post *postbox) {
	client.Watch(ctx, client.WatchParams{
		Open:    client.OpenWatch(watches),
		OnBatch: func(batch client.WatchBatch) { post.send(batch) },
		OnLost: func(err error) {
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "the watch stream ended", logging.Err(err)...)

			post.send(watchLostMsg{err: err})
		},
	})
}

// applyWatch takes a batch off the stream into the world, a snapshot replacing it whole, and
// draws the listing from it again.
func (m *model) applyWatch(batch client.WatchBatch) tea.Cmd {
	var changed assemble.Changed
	listed := m.withListing(func(l *listing) {
		for _, event := range batch {
			if _, snapshot := event.GetEvent().(*watchv1.WatchEvent_Snapshot); snapshot {
				m.reportUnreachable(nil)
			}

			changed.Merge(l.world.Apply(event))
		}

		l.assemble(time.Now())
	})

	return tea.Batch(listed, m.react(changed))
}

// applyWatchLost says the server is gone until the next stream's snapshot says otherwise, and
// says nothing of a close the service chose, which the reopen follows.
func (m *model) applyWatchLost(msg watchLostMsg) {
	if msg.err == nil {
		return
	}

	m.reportUnreachable(msg.err)
}
