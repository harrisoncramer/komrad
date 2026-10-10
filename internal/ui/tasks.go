package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
)

// taskAgents is the live agents on a task, oldest first.
func (l listing) taskAgents(taskID string) []panel.AgentRow {
	return xslice.Filter(l.agents, func(item panel.AgentRow) bool { return item.Agent.TaskID == taskID })
}

func taskFields(task panel.Task, agents []panel.AgentRow) []recordField {
	fields := []recordField{
		{label: "task", value: task.Slug},
		{label: "title", value: label.OrDash(task.Title)},
		{label: "agents", value: fmt.Sprintf("%d", len(agents))},
		{label: "task_id", value: task.ID},
	}
	if created := label.Friendly(task.CreatedAt); created != "" {
		fields = append(fields, recordField{label: "created", value: created})
	}

	return fields
}

// taskMarkdown is a task's goal with the agents working on it, each with its branch and pull
// request.
func taskMarkdown(task panel.Task, agents []panel.AgentRow) string {
	goal := task.Goal
	if strings.TrimSpace(goal) == "" {
		goal = "_no goal set_"
	}

	lines := xslice.Map(agents, func(item panel.AgentRow) string {
		parts := []string{item.Agent.Name}
		if item.Agent.Role != "" {
			parts = append(parts, string(item.Agent.Role))
		}
		if item.Agent.Branch != nil {
			parts = append(parts, "on `"+*item.Agent.Branch+"`")
		}
		if len(item.PullRequests) > 0 {
			current := item.PullRequests[0]
			parts = append(parts, current.Ref()+" "+pullRequestWords(current))
		}

		return "- " + strings.Join(parts, ", ")
	})

	return renderDocument(task.Title, "", goal, strings.Join(lines, "\n"))
}

// archiveTask archives a task along with the live agents on it.
func (m *model) archiveTask(task panel.Task) tea.Cmd {
	agents := xslice.Map(m.listing.taskAgents(task.ID), func(item panel.AgentRow) panel.Agent { return item.Agent })
	pending := m.unstageAhead(agents)

	return pending.attach(m.writeCmd(
		"archiving "+task.Slug,
		fmt.Sprintf("Could not archive task %q", task.Slug),
		func(a api) error {
			return pending.noteArchive(a.archiveTask(task.ID))
		},
	))
}
