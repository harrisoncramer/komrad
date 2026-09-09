package ui

import (
	"fmt"
	"strings"

	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}

	return v
}

type recordField struct {
	label string
	value string
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

func workflowFields(w workflowstore.Workflow, agentCount int) []recordField {
	fields := []recordField{
		{"name", orDash(w.Name)},
		{"agents", fmt.Sprintf("%d", agentCount)},
	}
	fields = append(fields, workflowConfigFields(w)...)

	return append(fields, recordField{"workflow_id", orDash(w.ID)})
}

func workflowConfigFields(w workflowstore.Workflow) []recordField {
	fields := []recordField{
		{"default agent", orDash(w.Harness)},
		{"directory", orDash(w.Cwd)},
		{"flags", orDash(w.Flags)},
	}

	if !w.Worktree.Enabled {
		return append(fields, recordField{"worktree", "off"})
	}

	fields = append(fields,
		recordField{"worktree", "on"},
		recordField{"base branch", orDash(w.Worktree.BaseBranch)},
	)
	if len(w.Worktree.CopyPaths) > 0 {
		fields = append(fields, recordField{"copy", strings.Join(w.Worktree.CopyPaths, ", ")})
	}
	if len(w.Worktree.SymlinkPaths) > 0 {
		fields = append(fields, recordField{"symlink", strings.Join(w.Worktree.SymlinkPaths, ", ")})
	}

	return fields
}

func workflowContextMarkdown(w workflowstore.Workflow) string {
	return renderDocument(w.Name, "", workflowProse(w), "")
}

func workflowProse(w workflowstore.Workflow) string {
	if strings.TrimSpace(w.Context) == "" {
		return "_no shared context set_"
	}

	return w.Context
}

func agentFields(agent agentstore.Agent, schedule, parent string) []recordField {
	fields := []recordField{{"name", orDash(agent.Name)}}
	// What a child is attached to comes early, and for a reviewer that same row is whose work it
	// is reading.
	reviewingDrawn := agent.ParentAgentID != nil && agent.Role == agentstore.RoleReviewer
	if agent.ParentAgentID != nil {
		fields = append(fields, recordField{attachmentFieldKey(agent.Role), orDash(parent)})
	}

	fields = append(fields,
		recordField{"agent", orDash(agent.Harness)},
		recordField{"directory", orDash(agent.Cwd)},
	)

	readBranch := agent.Branch != nil
	if readBranch {
		fields = append(fields, recordField{branchFieldKey, *agent.Branch})
	}

	fields = append(fields,
		recordField{"flags", orDash(agent.Flags)},

		recordField{"agent_id", orDash(agent.ID)},
		recordField{"session_id", orDash(agent.SessionID)},
	)
	if schedule != "" {
		fields = append(fields, recordField{"launched by", schedule})
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, recordField{"created", created})
	}
	for _, entry := range agent.Metadata {
		if entry.Key == failureReasonKey {
			continue
		}
		if entry.Key == branchFieldKey && readBranch {
			continue
		}
		if entry.Key == reviewingFieldKey && reviewingDrawn {
			continue
		}

		fields = append(fields, recordField{entry.Key, entry.Value})
	}

	return fields
}

// attachmentFieldKey labels the agent a child hangs off, which for a reviewer is the work it was
// launched to read rather than only where it happens to be running.
func attachmentFieldKey(role agentstore.Role) string {
	if role == agentstore.RoleReviewer {
		return reviewingFieldKey
	}

	return "attached to"
}

// reviewingFieldKey labels whose work a reviewer is reading.
const reviewingFieldKey = "reviewing"

// branchFieldKey labels the branch an agent's checkout is on.
const branchFieldKey = "branch"

const noSchedulesMarkdown = `# Schedules

A schedule launches an agent on a cron expression.

Press the new key to write one.
`

func scheduleFields(sched schedulestore.Schedule, workflow *workflowstore.Workflow) []recordField {
	fields := []recordField{
		{"name", orDash(sched.Name)},
		{"cron", orDash(sched.Cron)},
		{"timezone", orDash(sched.Timezone)},
	}

	if workflow == nil {
		return append(fields, recordField{"workflow", "— (archived)"})
	}

	fields = append(fields, recordField{"workflow", workflow.Name})

	return append(fields, workflowConfigFields(*workflow)...)
}

func scheduleMarkdown(item scheduleItem) string {
	sched := item.schedule

	notice := "_paused — will not run_"
	if sched.Enabled {
		notice = fmt.Sprintf("_next run %s_", orDash(dates.Friendly(item.nextRun)))
	}
	if result := lastResultNotice(sched); result != "" {
		notice += "\n" + result
	}

	prose := "_no prompt set_"
	if strings.TrimSpace(sched.Prompt) != "" {
		prose = sched.Prompt
	}

	var footer string
	if sched.HideAgents {
		footer = "Its runs start hidden."
	}

	return renderDocument(sched.Name, notice, prose, footer)
}

func lastResultNotice(sched schedulestore.Schedule) string {
	if sched.LastResultStatus == "" || sched.LastResultAt == nil {
		return ""
	}

	notice := fmt.Sprintf("_last result %s: %s_", dates.Friendly(*sched.LastResultAt), sched.LastResultStatus)
	if sched.LastResultMessage != "" {
		notice += ", " + sched.LastResultMessage
	}

	return notice
}

const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

func archiveMarkdown(agent agentstore.Agent) string {
	return renderDocument(agent.Name, "_archived — kept until deleted_", "",
		"Restore brings it back and picks the conversation up where it stopped.")
}

func archiveFields(agent agentstore.Agent, schedule string) []recordField {
	fields := []recordField{
		{"name", orDash(agent.Name)},
		{"agent", orDash(agent.Harness)},
		{"directory", orDash(agent.Cwd)},
		{"flags", orDash(agent.Flags)},
		{"agent_id", orDash(agent.ID)},
		{"session_id", orDash(agent.SessionID)},
	}
	if schedule != "" {
		fields = append(fields, recordField{"launched by", schedule})
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, recordField{"created", created})
	}
	if agent.ArchivedAt != nil {
		if archived := dates.Friendly(*agent.ArchivedAt); archived != "" {
			fields = append(fields, recordField{"archived", archived})
		}
	}

	return fields
}
