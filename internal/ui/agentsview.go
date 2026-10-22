package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
)

func (a *agentsScreen) details(m *model) details {
	r, ok := a.hoveredRow(m)
	if !ok {
		return details{}
	}

	if r.isTask() {
		return taskDetails(m, r)
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.listing.agentCount(r.workflow.ID)),
		}
	}

	return agentDetails(m, r)
}

// taskDetails is the record for a task's row, with the rule's tags too while the task is live.
func taskDetails(m *model, r agentRow) details {
	var tags []string
	if r.task.ArchivedAt == nil {
		tags = taskDetailsTags(m, *r.task)
	}
	var writerFields []recordField
	if writer, ok := r.writer(); ok {
		writerFields = append([]recordField{{label: "agent", value: label.OrDash(writer.Agent.Harness)}},
			sessionFields(writer.Agent, m.listing.scheduleName(writer.Agent))...)
	}

	return details{
		id:      r.task.ID,
		notices: agentNotices(m.glyphs, m.loading.frame(), m.listing.tasks, r),
		fields:  taskFields(m.buildCheckoutParams(*r.task), writerFields),
		tags:    tags,
	}
}

// agentDetails is the record for an agent's row, carrying its task's checkout where no task
// header stands over it and the rule's tags too while the agent is live.
func agentDetails(m *model, r agentRow) details {
	task := m.listing.tasks.findForAgent(r.item.Agent)
	params := agentFieldsParams{
		agent:    r.item.Agent,
		task:     task.Slug,
		schedule: m.listing.scheduleName(r.item.Agent),
	}
	var tags []string
	if r.underTask {
		params.headerCwd = task.Cwd
	} else {
		params.checkout = checkoutFields(m.buildCheckoutParams(task))
		if r.item.Agent.ArchivedAt == nil {
			tags = taskDetailsTags(m, task)
		}
	}

	return details{
		id:      r.item.Agent.ID,
		notices: agentNotices(m.glyphs, m.loading.frame(), m.listing.tasks, r),
		fields:  agentFields(params),
		tags:    tags,
	}
}

// doc is the markdown rendered beside the list for a row with no pane of its own.
func (a *agentsScreen) doc(m *model) string {
	r, ok := a.hoveredRow(m)
	item, held := r.paneItem()

	switch {
	case !ok:
		return ""
	case a.isShowingNotes(r):
		return taskMarkdown(m.listing.tasks[a.notes])
	case r.isTask() && !held:
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

func firstKey(bound []string, fallback string) string {
	if len(bound) == 0 {
		return fallback
	}

	return keys.Label(bound[:1])
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

func (a *agentsScreen) snapshot(m *model, pane list.Pane) view.Snapshot {
	layout := a.layout(m)
	work := splitChosen(m.listing, chosenAgents(a.Selected, layout.cursorRows()))

	return agentsSnapshot(snapshotParams{
		rows: layout.tree,
		triage: triageSnapshot{
			height:  a.triage.capacity,
			zone:    layout.zone,
			waiting: layout.waiting,
		},
		selected:     a.Selected,
		wholeTasks:   taskIDs(work.wholeTasks),
		scheduled:    m.listing.scheduledWorkflows(),
		workflowDirs: m.listing.workflowDirs(),
		focused:      list.KeyAt(layout.cursorRows(), a.Row),
		caret:        pane.Caret,
		loaderFrame:  m.loading.frame(),
		matcher:      m.filter.pattern,
		picked:       pickedRows(m.filter, layout.cursorRows(), m.agentRowFits),
		tasks:        m.listing.tasks,
		renaming:     a.renamingField(),
		showHidden:   a.showHidden,
		flat:         a.sort.flat,
		pane:         pane.Width,
	})
}
