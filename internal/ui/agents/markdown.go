package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
)

func workflowFields(w panel.Workflow, agentCount int) []record.Field {
	fields := []record.Field{
		{Label: "name", Value: label.OrDash(w.Name)},
		{Label: "agents", Value: fmt.Sprintf("%d", agentCount)},
	}
	fields = append(fields, record.WorkflowConfigFields(w)...)

	return append(fields,
		record.Field{Label: "reviewer agent", Value: label.OrDash(w.ReviewerHarness)},
		record.Field{Label: "reviewer flags", Value: label.OrDash(w.ReviewerFlags)},
		record.Field{Label: "workflow_id", Value: label.OrDash(w.ID)},
	)
}

func workflowContextMarkdown(w panel.Workflow) string {
	return paint.DocumentMarkdown(w.Name, "", workflowProse(w), "")
}

func workflowProse(w panel.Workflow) string {
	if strings.TrimSpace(w.Context) == "" {
		return "_no shared context set_"
	}

	return w.Context
}

// agentFieldsParams is an agent and what the record says about it that is not on its row: the
// name of what launched it, and its task's directory and checkout.
type agentFieldsParams struct {
	agent panel.Agent
	// task is the slug of the task the agent works on, empty for one on none.
	task     string
	schedule string
	// headerCwd is the directory of the task header standing over the agent, empty where none does.
	headerCwd string
	// checkout is its task's checkout rows, for a record no task header stands over.
	checkout []record.Field
}

func agentFields(params agentFieldsParams) []record.Field {
	agent, schedule := params.agent, params.schedule
	var fields []record.Field
	if params.task != "" {
		fields = append(fields, record.Field{Label: "task", Value: params.task})
	}

	fields = append(fields, record.Field{Label: "agent", Value: label.OrDash(agent.Harness)})
	if agent.Cwd != "" && agent.Cwd != params.headerCwd {
		fields = append(fields, record.Field{Label: "directory", Value: agent.Cwd})
	}
	fields = append(fields, params.checkout...)
	fields = append(fields, sessionFields(agent, schedule)...)
	fields = append(fields, record.Field{Label: "hooks", Value: label.OrDash(strings.Join(agent.Hooks, ", "))})
	if agent.Tool != "" {
		fields = append(fields, record.Field{Label: "tool", Value: agent.Tool})
	}
	if created := label.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, record.Field{Label: "created", Value: created})
	}
	if agent.ArchivedAt != nil {
		if archived := label.Friendly(*agent.ArchivedAt); archived != "" {
			fields = append(fields, record.Field{Label: "archived", Value: archived})
		}
	}

	return fields
}

// sessionFields is what the record says of an agent's conversation: how it was launched and the ids
// it goes by.
func sessionFields(agent panel.Agent, schedule string) []record.Field {
	fields := []record.Field{
		{Label: "flags", Value: label.OrDash(agent.Flags)},
		{Label: "agent_id", Value: label.OrDash(agent.ID)},
		{Label: "session_id", Value: label.OrDash(agent.SessionID)},
	}
	if schedule != "" {
		fields = append(fields, record.Field{Label: "launched by", Value: schedule})
	}

	return fields
}

const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nKomrad keeps the agents you archive here until you delete them.\n"

const emptyDeletedMarkdown = "# Deleted\n\n_nothing deleted_\n\nKomrad lists the archived tasks whose worktree was deleted here. Their records stay until you delete them too.\n"

func archiveMarkdown(agent panel.Agent) string {
	return paint.DocumentMarkdown(agent.Name, "_archived — kept until deleted_", "",
		"Restore brings it back and picks the conversation up where it stopped.")
}
