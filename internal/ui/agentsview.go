package ui

import (
	"fmt"
	"time"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	agentv1 "komrad/proto/gen/agent/v1"
)

func (a *agentsScreen) Details(in screen.Inputs) record.Details {
	r, ok := a.HoveredRow(in)
	if !ok {
		return record.Details{}
	}

	if r.isTask() {
		return taskDetails(in, a.deps.clock.Now(), a.deps.glyphs, r)
	}

	if r.header {
		return record.Details{
			ID:     r.workflow.ID,
			Fields: workflowFields(r.workflow, in.Listing.AgentCount(r.workflow.ID)),
		}
	}

	return agentDetails(in, a.deps.clock.Now(), a.deps.glyphs, r)
}

// taskDetails is the record for a task's row, with the rule's tags too while the task is live.
func taskDetails(in screen.Inputs, now time.Time, glyphs glyph.Set, r agentRow) record.Details {
	var tags []string
	if r.task.ArchivedAt == nil {
		tags = taskDetailsTags(in.Inspections, *r.task)
	}
	var writerFields []record.Field
	if writer, ok := r.writer(); ok {
		writerFields = append([]record.Field{{Label: "agent", Value: label.OrDash(writer.Agent.Harness)}},
			sessionFields(writer.Agent, in.Listing.ScheduleName(writer.Agent))...)
	}

	return record.Details{
		ID:      r.task.ID,
		Notices: agentNotices(glyphs, in.Room.Spinner, in.Listing.Tasks, r),
		Fields:  taskFields(buildCheckoutParams(in.Inspections, now, *r.task), writerFields),
		Tags:    tags,
	}
}

// agentDetails is the record for an agent's row, carrying its task's checkout where no task
// header stands over it and the rule's tags too while the agent is live.
func agentDetails(in screen.Inputs, now time.Time, glyphs glyph.Set, r agentRow) record.Details {
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
		params.checkout = checkoutFields(buildCheckoutParams(in.Inspections, now, task))
		if r.item.Agent.ArchivedAt == nil {
			tags = taskDetailsTags(in.Inspections, task)
		}
	}

	return record.Details{
		ID:      r.item.Agent.ID,
		Notices: agentNotices(glyphs, in.Room.Spinner, in.Listing.Tasks, r),
		Fields:  agentFields(params),
		Tags:    tags,
	}
}

// doc is the markdown rendered beside the list for a row with no pane of its own.
func (a *agentsScreen) doc(in screen.Inputs) string {
	r, ok := a.HoveredRow(in)
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
	case item.Agent.State == agentv1.AgentState_AGENT_STATE_SETTING_UP && item.Agent.Pane != "":
		return startingPlaceholder(in.Room.Spinner)
	case item.Agent.State == agentv1.AgentState_AGENT_STATE_SETTING_UP:
		return settingUpPlaceholder(in.Room.Spinner)
	case panel.IsFailed(item.Agent.State):
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
	if agent.State == agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP {
		label = "teardown"
	}

	reason := agent.FailureReason
	if reason == "" {
		reason = "no reason was recorded"
	}

	return fmt.Sprintf("_%s failed_\n\n%s", label, reason)
}

func (a *agentsScreen) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	layout := a.layout(in)
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
		matcher:      in.Room.Pattern.Regexp,
		picked:       list.Picked(in.Room.Pattern, layout.cursorRows(), agentRowFits(in)),
		tasks:        in.Listing.Tasks,
		renaming:     a.renamingField(),
		showHidden:   a.showHidden,
		flat:         a.sort.Flat,
		pane:         pane.Width,
	})
}
