package ui

import (
	"context"
	"errors"
	"log/slog"

	"weave/internal/client"
	"weave/internal/logging"
	watchv1 "weave/proto/gen/watch/v1"
	"weave/proto/gen/watch/v1/watchv1connect"

	tea "github.com/charmbracelet/bubbletea"
)

// errWatchClosed is what a stream the service closed on its own is reported as.
var errWatchClosed = errors.New("the watch stream closed")

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

// applyWatch takes a batch off the stream: a snapshot replaces the three listings, and every
// other event replaces or drops the row it names.
func (m *model) applyWatch(batch client.WatchBatch) tea.Cmd {
	return m.withListing(func(l *listing) {
		for _, event := range batch.Events {
			if _, snapshot := event.GetEvent().(*watchv1.WatchEvent_Snapshot); snapshot {
				m.reportUnreachable(nil)
			}

			l.apply(event)
		}
	})
}

func (l *listing) apply(event *watchv1.WatchEvent) {
	switch row := event.GetEvent().(type) {
	case *watchv1.WatchEvent_Snapshot:
		l.agents, l.workflows, l.schedules, l.marks = nil, nil, nil, nil
		for _, agent := range row.Snapshot.GetAgents() {
			l.putAgent(agent)
		}
		for _, workflow := range row.Snapshot.GetWorkflows() {
			l.putWorkflow(workflow)
		}
		for _, schedule := range row.Snapshot.GetSchedules() {
			l.putSchedule(schedule)
		}
	case *watchv1.WatchEvent_Agent:
		l.putAgent(row.Agent)
	case *watchv1.WatchEvent_Workflow:
		l.putWorkflow(row.Workflow)
	case *watchv1.WatchEvent_Schedule:
		l.putSchedule(row.Schedule)
	case *watchv1.WatchEvent_Deleted:
		switch row.Deleted.GetKind() {
		case watchv1.RowKind_ROW_KIND_AGENT:
			l.dropAgent(row.Deleted.GetId())
		case watchv1.RowKind_ROW_KIND_WORKFLOW:
			l.dropWorkflow(row.Deleted.GetId())
		case watchv1.RowKind_ROW_KIND_SCHEDULE:
			l.dropSchedule(row.Deleted.GetId())
		case watchv1.RowKind_ROW_KIND_UNSPECIFIED:
		}
	case *watchv1.WatchEvent_Heartbeat, nil:
	}
}

// applyWatchLost says the server is gone until the next stream's snapshot says otherwise.
func (m *model) applyWatchLost(msg watchLostMsg) {
	err := msg.err
	if err == nil {
		err = errWatchClosed
	}

	m.reportUnreachable(err)
}
