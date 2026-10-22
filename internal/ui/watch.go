package ui

import (
	"context"
	"log/slog"
	"time"

	"komrad/internal/assemble"
	"komrad/internal/client"
	"komrad/internal/logging"
	"komrad/internal/ui/panel"
	watchv1 "komrad/proto/gen/watch/v1"
	"komrad/proto/gen/watch/v1/watchv1connect"

	tea "charm.land/bubbletea/v2"
)

// watchLostMsg says the stream has ended and is being opened again.
type watchLostMsg struct {
	err error
}

// startWatch opens the watch on a goroutine of its own and hands back what stops it.
func startWatch(ctx context.Context, watches watchv1connect.WatchServiceClient, post *postbox) context.CancelFunc {
	if watches == nil {
		return func() {}
	}

	watching, stop := context.WithCancel(ctx)
	go watchRows(watching, watches, post)

	return stop
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
// renders the listing from it again.
func (m *model) applyWatch(batch client.WatchBatch) tea.Cmd {
	if m.isHoldingWatch() {
		m.writes.deferred = append(m.writes.deferred, batch...)

		return nil
	}

	return m.takeWatch(batch)
}

func (m *model) takeWatch(batch client.WatchBatch) tea.Cmd {
	var changed assemble.Changed

	var reread tea.Cmd
	if m.unreachable != nil {
		reread = m.readEngineBuildCmd()
	}

	listed := m.withListing(func(l *panel.Listing) {
		for _, event := range batch {
			if _, snapshot := event.GetEvent().(*watchv1.WatchEvent_Snapshot); snapshot {
				m.reportUnreachable(nil)
			}

			changed.Merge(l.World.Apply(event))
		}

		l.Assemble(time.Now())
	})

	return tea.Batch(listed, m.react(changed), reread)
}

// applyWatchLost says the server is gone until the next stream's snapshot says otherwise, and
// says nothing of a close the server chose, which the reopen follows.
func (m *model) applyWatchLost(msg watchLostMsg) tea.Cmd {
	if msg.err == nil {
		return nil
	}

	m.reportUnreachable(msg.err)

	if m.access.signIn == nil {
		return nil
	}

	return m.checkAccessCmd()
}

func (m *model) isHoldingWatch() bool {
	if !m.writes.held.active {
		return false
	}

	_, parked := m.writes.parked[m.writes.held.write]

	return !parked
}

func (m *model) releaseWatch(landing bool) tea.Cmd {
	if len(m.writes.deferred) == 0 || (!landing && m.isHoldingWatch()) {
		return nil
	}

	batch := m.writes.deferred
	m.writes.deferred = nil

	return m.takeWatch(batch)
}
