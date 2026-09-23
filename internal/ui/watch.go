package ui

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"weave/internal/client"
	"weave/internal/logging"
	"weave/internal/ui/panel"
	watchv1 "weave/proto/gen/watch/v1"
	"weave/proto/gen/watch/v1/watchv1connect"

	tea "github.com/charmbracelet/bubbletea"
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
	var arrived []string
	listed := m.withListing(func(l *listing) {
		for _, event := range batch {
			if _, snapshot := event.GetEvent().(*watchv1.WatchEvent_Snapshot); snapshot {
				m.reportUnreachable(nil)
			}
			if l.doesMoveArchive(event) {
				m.archiveReads.current = false
			}

			arrived = append(arrived, l.world.Apply(event)...)
		}

		l.assemble(time.Now())
	})

	return tea.Batch(listed, m.listArrivedPullRequestsCmd(arrived))
}

// doesMoveArchive reports whether an event could change what the archive holds.
func (l listing) doesMoveArchive(event *watchv1.WatchEvent) bool {
	switch event := event.GetEvent().(type) {
	case *watchv1.WatchEvent_Snapshot:
		return true
	case *watchv1.WatchEvent_Changes:
		for _, row := range event.Changes.GetPuts() {
			if agent := row.GetAgent(); agent != nil && l.world.Agents[agent.GetId()] == nil {
				return true
			}
			if workflow := row.GetWorkflow(); workflow != nil && l.world.Workflows[workflow.GetId()] == nil {
				return true
			}
		}

		return slices.ContainsFunc(event.Changes.GetDeleted(), l.doesKnow)
	}

	return false
}

// doesKnow reports whether a gone key names an agent or workflow the world or the archive holds.
func (l listing) doesKnow(deleted *watchv1.Deleted) bool {
	key := deleted.GetKey()

	switch deleted.GetResource() {
	case watchv1.Resource_RESOURCE_AGENT:
		isKey := func(row panel.AgentRow) bool {
			return row.Agent.ID == key
		}

		return l.world.Agents[key] != nil || slices.ContainsFunc(l.archivedAgents, isKey)
	case watchv1.Resource_RESOURCE_WORKFLOW:
		isKey := func(workflow panel.Workflow) bool {
			return workflow.ID == key
		}

		return l.world.Workflows[key] != nil || slices.ContainsFunc(l.archivedWorkflows, isKey)
	case watchv1.Resource_RESOURCE_UNSPECIFIED,
		watchv1.Resource_RESOURCE_SCHEDULE,
		watchv1.Resource_RESOURCE_MARK,
		watchv1.Resource_RESOURCE_MAIL,
		watchv1.Resource_RESOURCE_PULL_REQUEST:
	}

	return false
}

// applyWatchLost says the server is gone until the next stream's snapshot says otherwise, and
// says nothing of a close the service chose, which the reopen follows.
func (m *model) applyWatchLost(msg watchLostMsg) {
	if msg.err == nil {
		return
	}

	m.reportUnreachable(msg.err)
}
