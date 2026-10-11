package ui

import (
	"fmt"
	"time"

	"komrad/config"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
)

func (a *agentsScreen) details(m *model) details {
	r, ok := a.hoveredRow(m)
	if !ok {
		return details{}
	}

	if r.isTask() {
		writer := m.listing.findTaskWriter(*r.task, *r.lead)
		worktree, read := agentWorktree(m.inspections, r.task.Cwd)
		var tags []string
		if item, ok := m.listing.findCheckoutWriter(*r.task, *r.lead); ok {
			tags = agentDetailsTags(m, item)
		}

		return details{
			id:      r.task.ID,
			notices: agentNotices(m.glyphs, m.loading.frame(), r),
			fields: taskFields(taskFieldsParams{
				task:     *r.task,
				agents:   m.listing.taskAgents(r.task.ID),
				writer:   writer,
				worktree: worktree,
				read:     read,
				now:      time.Now(),
			}),
			tags: tags,
		}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.listing.agentCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.Agent.ID,
		notices: agentNotices(m.glyphs, m.loading.frame(), r),
		fields:  agentDetailFields(m, r.item),
		tags:    agentDetailsTags(m, r.item),
	}
}

// agentDetailFields is the record for an agent as the listing holds it.
func agentDetailFields(m *model, item panel.AgentRow) []recordField {
	worktree, read := agentWorktree(m.inspections, item.Agent.Cwd)

	return agentFields(agentFieldsParams{
		agent:        item.Agent,
		schedule:     m.listing.scheduleName(item.Agent),
		parent:       m.listing.parentName(item.Agent),
		standing:     item.PullRequest,
		pullRequests: item.PullRequests,
		worktree:     worktree,
		read:         read,
		now:          time.Now(),
	})
}

// doc is the markdown rendered beside the list for a row with no pane of its own.
func (a *agentsScreen) doc(m *model) string {
	r, ok := a.hoveredRow(m)
	item, held := r.paneItem()

	switch {
	case !ok:
		return ""
	case r.isTask():
		return taskMarkdown(*r.task)
	case r.header && !held:
		return workflowContextMarkdown(r.workflow)
	case item.Agent.State == panel.StateSettingUp && item.Agent.Pane != "":
		return startingPlaceholder(m.loading.frame())
	case item.Agent.State == panel.StateSettingUp:
		return settingUpPlaceholder(m.loading.frame())
	case item.Agent.HeldAt != nil:
		return heldPlaceholder
	case item.Agent.State.IsFailed():
		return failedPlaceholder(item.Agent)
	}

	return ""
}

func welcomeText(keys config.Keybindings) string {
	return fmt.Sprintf("Welcome, komrad!\n\nPress '%s' then '%s' to set up your first workflow", firstKey(keys.Menu.Open, "space"), firstKey(keys.Menu.NewWorkflow, "w"))
}

func firstKey(keys []string, fallback string) string {
	if len(keys) == 0 {
		return fallback
	}

	return keyLabel(keys[:1])
}

func settingUpPlaceholder(frame string) string {
	return frame + " _setting up…_\n\nMaking this agent's worktree. It has no pane yet."
}

func startingPlaceholder(frame string) string {
	return frame + " _starting…_\n\nIts CLI is coming up in a new pane."
}

func failedPlaceholder(agent panel.Agent) string {
	label := "setup"
	if agent.State == panel.StateFailedToCleanup {
		label = "teardown"
	}

	reason := agent.FailureReason
	if reason == "" {
		reason = "no reason was recorded"
	}

	return fmt.Sprintf("_%s failed_\n\n%s", label, reason)
}

func (a *agentsScreen) view(m *model) string {
	if m.listing.isEmpty() {
		return m.welcomeView(a.listParams(m), welcomeText(m.keys))
	}

	return m.columnListView(a.listParams(m))
}

func (a *agentsScreen) snapshot(m *model, pane listPane) view.Snapshot {
	layout := a.layout(m)
	work := splitChosen(m.listing, chosenAgents(a.selected, layout.cursorRows()))

	return agentsSnapshot(snapshotParams{
		rows: layout.tree,
		triage: triageSnapshot{
			height:  a.triage.capacity,
			zone:    layout.zone,
			waiting: layout.waiting,
		},
		selected:     a.selected,
		wholeTasks:   taskIDs(work.wholeTasks),
		scheduled:    m.listing.scheduledWorkflows(),
		workflowDirs: m.listing.workflowDirs(),
		focused:      rowKeyAt(layout.cursorRows(), a.cursor),
		caret:        pane.caret,
		loaderFrame:  m.loading.frame(),
		matcher:      m.filter.pattern,
		picked:       pickedRows(m.filter, layout.cursorRows(), m.agentRowFits),
		renaming:     a.renamingField(),
		showHidden:   a.showHidden,
		flat:         a.sort.flat,
		pane:         pane.width,
	})
}
