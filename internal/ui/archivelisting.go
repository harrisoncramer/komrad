package ui

import (
	"slices"

	"weave/internal/ui/panel"
	watchv1 "weave/proto/gen/watch/v1"
)

// archiveListingState is whether the archive the screens draw from needs reading again.
type archiveListingState struct {
	// upToDate is set once the archive is read and nothing the watch has seen since could change it.
	upToDate bool
	// readInFlight is set while a read of the archive has not yet landed.
	readInFlight bool
}

// shouldRead reports whether a screen that draws the archive should read it now.
func (s archiveListingState) shouldRead() bool {
	return !s.upToDate && !s.readInFlight
}

// markReadStarted records a read of the archive as in flight.
func (s *archiveListingState) markReadStarted() {
	s.upToDate = true
	s.readInFlight = true
}

// markReadFinished records a read of the archive landing, and leaves it out of date when the read failed.
func (s *archiveListingState) markReadFinished(failed bool) {
	s.readInFlight = false
	if failed {
		s.upToDate = false
	}
}

// markOutOfDate records that the archive needs reading again.
func (s *archiveListingState) markOutOfDate() {
	s.upToDate = false
}

// markOutOfDateIfEventMayChangeIt records that the archive needs reading again when a watch event
// could have changed what it holds.
func (s *archiveListingState) markOutOfDateIfEventMayChangeIt(l *listing, event *watchv1.WatchEvent) {
	if mayEventChangeArchive(l, event) {
		s.markOutOfDate()
	}
}

// mayEventChangeArchive reports whether a watch event could change what the archive holds.
func mayEventChangeArchive(l *listing, event *watchv1.WatchEvent) bool {
	switch event := event.GetEvent().(type) {
	case *watchv1.WatchEvent_Snapshot:
		return true
	case *watchv1.WatchEvent_Changes:
		for _, row := range event.Changes.GetPuts() {
			if isAgentOrWorkflowNewToWorld(l, row) {
				return true
			}
		}

		for _, deleted := range event.Changes.GetDeleted() {
			if isDeletedRowLiveOrArchived(l, deleted) {
				return true
			}
		}
	}

	return false
}

// isAgentOrWorkflowNewToWorld reports whether a row is an agent or workflow the world did not hold.
func isAgentOrWorkflowNewToWorld(l *listing, row *watchv1.Row) bool {
	if agent := row.GetAgent(); agent != nil {
		return l.world.Agents[agent.GetId()] == nil
	}

	if workflow := row.GetWorkflow(); workflow != nil {
		return l.world.Workflows[workflow.GetId()] == nil
	}

	return false
}

// isDeletedRowLiveOrArchived reports whether a deleted key names an agent or workflow the world
// or the archive holds.
func isDeletedRowLiveOrArchived(l *listing, deleted *watchv1.Deleted) bool {
	deletedID := deleted.GetKey()

	switch deleted.GetResource() {
	case watchv1.Resource_RESOURCE_AGENT:
		isDeletedAgent := func(row panel.AgentRow) bool {
			return row.Agent.ID == deletedID
		}

		return l.world.Agents[deletedID] != nil || slices.ContainsFunc(l.archivedAgents, isDeletedAgent)
	case watchv1.Resource_RESOURCE_WORKFLOW:
		isDeletedWorkflow := func(workflow panel.Workflow) bool {
			return workflow.ID == deletedID
		}

		return l.world.Workflows[deletedID] != nil || slices.ContainsFunc(l.archivedWorkflows, isDeletedWorkflow)
	case watchv1.Resource_RESOURCE_UNSPECIFIED,
		watchv1.Resource_RESOURCE_SCHEDULE,
		watchv1.Resource_RESOURCE_MARK,
		watchv1.Resource_RESOURCE_MAIL,
		watchv1.Resource_RESOURCE_PULL_REQUEST:
	}

	return false
}
