package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	schedulev1 "komrad/proto/gen/schedule/v1"
)

type recordField struct {
	label  string
	value  string
	styled string
}

func renderDocument(title, notice, prose, footer string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if notice != "" {
		b.WriteString(notice + "\n\n")
	}
	if prose != "" {
		b.WriteString(prose + "\n")
	}
	if footer != "" {
		b.WriteString("\n" + footer + "\n")
	}

	return b.String()
}

func workflowFields(w panel.Workflow, agentCount int) []recordField {
	fields := []recordField{
		{label: "name", value: label.OrDash(w.Name)},
		{label: "agents", value: fmt.Sprintf("%d", agentCount)},
	}
	fields = append(fields, workflowConfigFields(w)...)

	return append(fields,
		recordField{label: "reviewer agent", value: label.OrDash(w.ReviewerHarness)},
		recordField{label: "reviewer flags", value: label.OrDash(w.ReviewerFlags)},
		recordField{label: "workflow_id", value: label.OrDash(w.ID)},
	)
}

func workflowConfigFields(w panel.Workflow) []recordField {
	fields := []recordField{
		{label: "default agent", value: label.OrDash(w.Harness)},
		{label: "directory", value: label.OrDash(w.Cwd)},
		{label: "flags", value: label.OrDash(w.Flags)},
	}

	if !w.Worktree.Enabled {
		return append(fields, recordField{label: "worktree", value: "off"})
	}

	fields = append(fields,
		recordField{label: "worktree", value: "on"},
		recordField{label: "base branch", value: label.OrDash(w.Worktree.BaseBranch)},
	)
	if len(w.Worktree.CopyPaths) > 0 {
		fields = append(fields, recordField{label: "copy", value: strings.Join(w.Worktree.CopyPaths, ", ")})
	}
	if len(w.Worktree.SymlinkPaths) > 0 {
		fields = append(fields, recordField{label: "symlink", value: strings.Join(w.Worktree.SymlinkPaths, ", ")})
	}

	return fields
}

func workflowContextMarkdown(w panel.Workflow) string {
	return renderDocument(w.Name, "", workflowProse(w), "")
}

func workflowProse(w panel.Workflow) string {
	if strings.TrimSpace(w.Context) == "" {
		return "_no shared context set_"
	}

	return w.Context
}

// agentFieldsParams is an agent and what the record says about it that is not on its row: the
// names of what launched it and what it hangs off, and its task's directory and checkout.
type agentFieldsParams struct {
	agent panel.Agent
	// task is the slug of the task the agent works on, empty for one on none.
	task     string
	schedule string
	parent   string
	// headerCwd is the directory of the task header standing over the agent, empty where none does.
	headerCwd string
	// checkout is its task's checkout rows, for a record no task header stands over.
	checkout []recordField
}

func agentFields(params agentFieldsParams) []recordField {
	agent, schedule, parent := params.agent, params.schedule, params.parent
	var fields []recordField
	// What a child is attached to comes early, and for a reviewer that same row is whose work it
	// is reading.
	if agent.ParentAgentID != nil {
		fields = append(fields, recordField{label: attachmentFieldKey(agent.Role), value: label.OrDash(parent)})
	}

	if params.task != "" {
		fields = append(fields, recordField{label: "task", value: params.task})
	}

	fields = append(fields, recordField{label: "agent", value: label.OrDash(agent.Harness)})
	if agent.Cwd != "" && agent.Cwd != params.headerCwd {
		fields = append(fields, recordField{label: "directory", value: agent.Cwd})
	}
	fields = append(fields, params.checkout...)

	fields = append(fields,
		recordField{label: "flags", value: label.OrDash(agent.Flags)},

		recordField{label: "agent_id", value: label.OrDash(agent.ID)},
		recordField{label: "session_id", value: label.OrDash(agent.SessionID)},
	)
	if schedule != "" {
		fields = append(fields, recordField{label: "launched by", value: schedule})
	}
	if created := label.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, recordField{label: "created", value: created})
	}
	if agent.ArchivedAt != nil {
		if archived := label.Friendly(*agent.ArchivedAt); archived != "" {
			fields = append(fields, recordField{label: "archived", value: archived})
		}
	}

	return fields
}

// attachmentFieldKey labels the agent a child hangs off, which for a reviewer is the work it was
// launched to read rather than only where it happens to be running.
func attachmentFieldKey(role panel.Role) string {
	if role == panel.RoleReviewer {
		return reviewingFieldKey
	}

	return "attached to"
}

// reviewingFieldKey labels whose work a reviewer is reading.
const reviewingFieldKey = "reviewing"

const noSchedulesMarkdown = `# Schedules

A schedule launches an agent, or mails an orchestrator, on a cron expression.

Press the new key to write one.
`

type scheduleTarget struct {
	workflow     *panel.Workflow
	orchestrator string
}

func scheduleFields(sched panel.Schedule, target scheduleTarget) []recordField {
	fields := []recordField{
		{label: "name", value: label.OrDash(sched.Name)},
		{label: "cron", value: label.OrDash(sched.Cron)},
		{label: "timezone", value: label.OrDash(sched.Timezone)},
	}

	if sched.Kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		return append(fields,
			recordField{label: "kind", value: "orchestrator"},
			recordField{label: "mails", value: target.orchestrator},
		)
	}

	if !knownScheduleKind(sched.Kind) {
		return append(fields, recordField{label: "kind", value: "unknown"})
	}

	fields = append(fields, recordField{label: "kind", value: "task"})
	if target.workflow == nil {
		return append(fields, recordField{label: "workflow", value: "— (archived)"})
	}

	fields = append(fields, recordField{label: "workflow", value: target.workflow.Name})

	return append(fields, workflowConfigFields(*target.workflow)...)
}

// scheduleDocRows is a schedule's document at a width: its name and next run, its firings, then
// its prompt.
func scheduleDocRows(item scheduleItem, width int) []string {
	sched := item.schedule

	notice := "_paused — will not run_"
	switch {
	case sched.ArchivedAt != nil:
		notice = "_archived — kept until deleted_"
	case sched.Enabled:
		notice = fmt.Sprintf("_next run %s_", label.OrDash(label.Friendly(item.nextRun)))
	}

	prose := "_no prompt set_"
	if strings.TrimSpace(sched.Prompt) != "" {
		prose = sched.Prompt
	}

	if sched.HideAgents {
		prose += "\n\nIts runs start hidden."
	}

	rows := paint.DocStyledRows(renderDocument(sched.Name, notice, "", ""), width, columnStyle())
	if runRows := scheduleRunRows(sched.RecentRuns, width); len(runRows) > 0 {
		rows = append(append(rows, ""), runRows...)
	}

	return append(rows, paint.DocStyledRows(prose+"\n", width, columnStyle())...)
}

const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

const emptyScheduleArchiveMarkdown = "# Archive\n\n_no archived schedules_\n\nSchedules archived from the panel are kept here until you delete them.\n"

func archiveMarkdown(agent panel.Agent) string {
	return renderDocument(agent.Name, "_archived — kept until deleted_", "",
		"Restore brings it back and picks the conversation up where it stopped.")
}
