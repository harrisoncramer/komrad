package ui

import (
	"fmt"
	"strings"
	"time"

	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
)

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

func workflowFields(w panel.Workflow, agentCount int) []recordField {
	fields := []recordField{
		{"name", label.OrDash(w.Name)},
		{"agents", fmt.Sprintf("%d", agentCount)},
	}
	fields = append(fields, workflowConfigFields(w)...)

	return append(fields, recordField{"workflow_id", label.OrDash(w.ID)})
}

func workflowConfigFields(w panel.Workflow) []recordField {
	fields := []recordField{
		{"default agent", label.OrDash(w.Harness)},
		{"directory", label.OrDash(w.Cwd)},
		{"flags", label.OrDash(w.Flags)},
	}

	if !w.Worktree.Enabled {
		return append(fields, recordField{"worktree", "off"})
	}

	fields = append(fields,
		recordField{"worktree", "on"},
		recordField{"base branch", label.OrDash(w.Worktree.BaseBranch)},
	)
	if len(w.Worktree.CopyPaths) > 0 {
		fields = append(fields, recordField{"copy", strings.Join(w.Worktree.CopyPaths, ", ")})
	}
	if len(w.Worktree.SymlinkPaths) > 0 {
		fields = append(fields, recordField{"symlink", strings.Join(w.Worktree.SymlinkPaths, ", ")})
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
// names of what launched it and what it hangs off, and the pull requests from its branch.
type agentFieldsParams struct {
	agent        panel.Agent
	schedule     string
	parent       string
	workflow     string
	standing     panel.PullRequestStanding
	pullRequests []panel.PullRequest
	// worktree is what the service has answered about the agent's directory, and read says
	// whether it has answered that the directory is a worktree at all.
	worktree worktreeInspection
	read     bool
	now      time.Time
}

func agentFields(params agentFieldsParams) []recordField {
	agent, schedule, parent := params.agent, params.schedule, params.parent
	fields := []recordField{{"name", label.OrDash(agent.Name)}}
	// What a child is attached to comes early, and for a reviewer that same row is whose work it
	// is reading.
	reviewingDrawn := agent.ParentAgentID != nil && agent.Role == panel.RoleReviewer
	if agent.ParentAgentID != nil {
		fields = append(fields, recordField{attachmentFieldKey(agent.Role), label.OrDash(parent)})
	}

	fields = append(fields,
		recordField{"agent", label.OrDash(agent.Harness)},
		recordField{"workflow", label.OrDash(params.workflow)},
		recordField{"directory", label.OrDash(agent.Cwd)},
	)

	if agent.ReportedReason != "" {
		fields = append(fields, recordField{"doing", agent.ReportedReason})
	}

	readBranch := agent.Branch != nil
	if readBranch {
		fields = append(fields, recordField{branchFieldKey, *agent.Branch})
		fields = append(fields, pullRequestFields(params.standing, params.pullRequests)...)
	}
	if params.read {
		fields = append(fields, worktreeStandingFields(params.worktree, params.now)...)
	}

	fields = append(fields,
		recordField{"flags", label.OrDash(agent.Flags)},

		recordField{"agent_id", label.OrDash(agent.ID)},
		recordField{"session_id", label.OrDash(agent.SessionID)},
	)
	if schedule != "" {
		fields = append(fields, recordField{"launched by", schedule})
	}
	if created := label.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, recordField{"created", created})
	}
	if agent.ArchivedAt != nil {
		if archived := label.Friendly(*agent.ArchivedAt); archived != "" {
			fields = append(fields, recordField{"archived", archived})
		}
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
	if params.read {
		fields = append(fields, worktreeHistoryFields(params.worktree)...)
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

// branchFieldKey labels the branch an agent's checkout is on.
const branchFieldKey = "branch"

const noSchedulesMarkdown = `# Schedules

A schedule launches an agent on a cron expression.

Press the new key to write one.
`

func scheduleFields(sched panel.Schedule, workflow *panel.Workflow) []recordField {
	fields := []recordField{
		{"name", label.OrDash(sched.Name)},
		{"cron", label.OrDash(sched.Cron)},
		{"timezone", label.OrDash(sched.Timezone)},
	}

	if workflow == nil {
		return append(fields, recordField{"workflow", "— (archived)"})
	}

	fields = append(fields, recordField{"workflow", workflow.Name})

	return append(fields, workflowConfigFields(*workflow)...)
}

// scheduleDocRows is a schedule's document at a width: its name and next run, its firings, then
// its prompt.
func scheduleDocRows(item scheduleItem, runs scheduleRunsRead, width int) []string {
	sched := item.schedule

	notice := "_paused — will not run_"
	if sched.Enabled {
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
	if runRows := scheduleRunRows(runs, width); len(runRows) > 0 {
		rows = append(append(rows, ""), runRows...)
	}

	return append(rows, paint.DocStyledRows(prose+"\n", width, columnStyle())...)
}

const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

func archiveMarkdown(agent panel.Agent) string {
	return renderDocument(agent.Name, "_archived — kept until deleted_", "",
		"Restore brings it back and picks the conversation up where it stopped.")
}
