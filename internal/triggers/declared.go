// Package triggers declares every trigger the schema carries and writes the files the binary applies them from.
package triggers

import (
	"cmp"
	"slices"

	"komrad/internal/watch"
	"komrad/internal/watched"
	"komrad/lib/xslice"
)

// triggerFunction is one function a trigger runs, with its plpgsql body.
type triggerFunction struct {
	Name string
	Body string
}

// rowTrigger is one row trigger on a table.
type rowTrigger struct {
	Name     string
	Timing   string
	Events   string
	Function string
	Args     []string
}

// triggeredTable is one table and every trigger it carries.
type triggeredTable struct {
	Name     string
	Triggers []rowTrigger
}

var setUpdatedAt = rowTrigger{
	Name:     "set_updated_at",
	Timing:   "BEFORE",
	Events:   "UPDATE",
	Function: "set_updated_at",
}

var declared = []triggeredTable{
	{Name: "agent_pull_requests"},
	{Name: "agent_runs"},
	{Name: "agent_screens"},
	{Name: "agent_transcript_chunks"},
	{Name: "agent_transcript_sessions"},
	{Name: "agents", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "document_type_versions"},
	{Name: "document_types", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "document_versions"},
	{Name: "documents", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "engine_access"},
	{Name: "engine_logins"},
	{Name: "engines"},
	{Name: "forge_connections"},
	{Name: "forge_review_comments", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "forge_review_threads", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "mail", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "pull_request_check_failure_notes"},
	{Name: "pull_request_check_log_steps"},
	{Name: "pull_request_check_logs"},
	{Name: "pull_request_check_readings"},
	{Name: "pull_request_check_steps"},
	{Name: "pull_request_checks"},
	{Name: "pull_request_syncs"},
	{Name: "pull_requests"},
	{Name: "review_forge_replies", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "review_messages", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "review_snapshots"},
	{Name: "review_threads", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "review_viewed_files"},
	{Name: "reviews", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "schedule_runs"},
	{Name: "schedules", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "task_pull_requests"},
	{Name: "tasks", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "workflow_configs", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "workflow_event_prompts"},
	{Name: "workflow_hooks"},
	{Name: "workflow_provisioner_configs", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "workflows", Triggers: []rowTrigger{setUpdatedAt}},
	{Name: "worktrees"},
}

var functions = []triggerFunction{
	{
		Name: "set_updated_at",
		Body: `
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
`,
	},
}

// tables lists every table in the schema with the triggers it carries, each table's sorted by name.
func tables() []triggeredTable {
	tables := xslice.Map(declared, func(table triggeredTable) triggeredTable {
		return triggeredTable{Name: table.Name, Triggers: slices.Clone(table.Triggers)}
	})
	indexOf := map[string]int{}
	for i, table := range tables {
		indexOf[table.Name] = i
	}

	for _, trigger := range watch.BuildTriggers(watched.All()) {
		i, ok := indexOf[trigger.Table]
		if !ok {
			i = len(tables)
			indexOf[trigger.Table] = i
			tables = append(tables, triggeredTable{Name: trigger.Table})
		}
		tables[i].Triggers = append(tables[i].Triggers, rowTrigger{
			Name:     trigger.Name,
			Timing:   "AFTER",
			Events:   "INSERT OR UPDATE OR DELETE",
			Function: trigger.Function,
			Args:     trigger.Args,
		})
	}

	for i := range tables {
		slices.SortFunc(tables[i].Triggers, func(a, b rowTrigger) int { return cmp.Compare(a.Name, b.Name) })
	}
	slices.SortFunc(tables, func(a, b triggeredTable) int { return cmp.Compare(a.Name, b.Name) })

	return tables
}

// listFunctions lists every function a trigger runs, sorted by name.
func listFunctions() []triggerFunction {
	all := append(slices.Clone(functions), xslice.Map(watch.TriggerFunctions, func(function watch.TriggerFunction) triggerFunction {
		return triggerFunction{Name: function.Name, Body: function.Body}
	})...)
	slices.SortFunc(all, func(a, b triggerFunction) int { return cmp.Compare(a.Name, b.Name) })

	return all
}
