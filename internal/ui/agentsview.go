package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
)

func (a *agentsScreen) details(m *model, in screen.Inputs) details {
	r, ok := a.hoveredRow(m, in)
	if !ok {
		return details{}
	}

	if r.isTask() {
		return taskDetails(m, in, a.deps.glyphs, r)
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, in.Listing.AgentCount(r.workflow.ID)),
		}
	}

	return agentDetails(m, in, a.deps.glyphs, r)
}

// taskDetails is the record for a task's row, with the rule's tags too while the task is live.
func taskDetails(m *model, in screen.Inputs, glyphs glyph.Set, r agentRow) details {
	var tags []string
	if r.task.ArchivedAt == nil {
		tags = taskDetailsTags(m, *r.task)
	}
	var writerFields []recordField
	if writer, ok := r.writer(); ok {
		writerFields = append([]recordField{{label: "agent", value: label.OrDash(writer.Agent.Harness)}},
			sessionFields(writer.Agent, in.Listing.ScheduleName(writer.Agent))...)
	}

	return details{
		id:      r.task.ID,
		notices: agentNotices(glyphs, in.Room.Spinner, in.Listing.Tasks, r),
		fields:  taskFields(m.buildCheckoutParams(*r.task), writerFields),
		tags:    tags,
	}
}

// agentDetails is the record for an agent's row, carrying its task's checkout where no task
// header stands over it and the rule's tags too while the agent is live.
func agentDetails(m *model, in screen.Inputs, glyphs glyph.Set, r agentRow) details {
	task := in.Listing.Tasks.FindForAgent(r.item.Agent)
	params := agentFieldsParams{
		agent:    r.item.Agent,
		task:     task.Slug,
		schedule: in.Listing.ScheduleName(r.item.Agent),
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
		notices: agentNotices(glyphs, in.Room.Spinner, in.Listing.Tasks, r),
		fields:  agentFields(params),
		tags:    tags,
	}
}

// doc is the markdown rendered beside the list for a row with no pane of its own.
func (a *agentsScreen) doc(m *model, in screen.Inputs) string {
	r, ok := a.hoveredRow(m, in)
	item, held := r.paneItem()

	switch {
	case !ok:
		return ""
	case a.isShowingNotes(r):
		return taskMarkdown(in.Listing.Tasks[a.notes])
	case r.isTask() && !held:
		return taskMarkdown(*r.task)
	case r.header && !held:
		return workflowContextMarkdown(r.workflow)
	case item.Agent.State == panel.StateSettingUp && item.Agent.Pane != "":
		return startingPlaceholder(in.Room.Spinner)
	case item.Agent.State == panel.StateSettingUp:
		return settingUpPlaceholder(in.Room.Spinner)
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

func (a *agentsScreen) snapshot(m *model, in screen.Inputs, pane list.Pane) view.Snapshot {
	layout := a.layout(m, in)
	work := splitChosen(in.Listing, chosenAgents(a.Selected, layout.cursorRows()))

	return agentsSnapshot(snapshotParams{
		rows: layout.tree,
		triage: triageSnapshot{
			height:  a.triage.capacity,
			zone:    layout.zone,
			waiting: layout.waiting,
		},
		selected:     a.Selected,
		wholeTasks:   panel.TaskIDs(work.wholeTasks),
		scheduled:    in.Listing.ScheduledWorkflows(),
		workflowDirs: in.Listing.WorkflowDirs(),
		focused:      list.KeyAt(layout.cursorRows(), a.Row),
		caret:        pane.Caret,
		loaderFrame:  in.Room.Spinner,
		matcher:      m.filter.pattern,
		picked:       pickedRows(m.filter, layout.cursorRows(), m.agentRowFits(in.Listing)),
		tasks:        in.Listing.Tasks,
		renaming:     a.renamingField(),
		showHidden:   a.showHidden,
		flat:         a.sort.flat,
		pane:         pane.Width,
	})
}
