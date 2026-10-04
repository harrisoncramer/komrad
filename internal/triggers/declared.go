// Package triggers declares every trigger the schema carries and writes the files the binary applies them from.
package triggers

import (
	"cmp"
	"slices"

	"komrad/internal/watch"
	"komrad/internal/watched"
	"komrad/lib/xslice"
)

// Function is one function a trigger runs, with its plpgsql body.
type Function struct {
	Name string
	Body string
}

// Trigger is one row trigger on a table.
type Trigger struct {
	Name     string
	Timing   string
	Events   string
	Function string
	Args     []string
}

// Table is one table and every trigger it carries.
type Table struct {
	Name     string
	Triggers []Trigger
}

var setUpdatedAt = Trigger{
	Name:     "set_updated_at",
	Timing:   "BEFORE",
	Events:   "UPDATE",
	Function: "set_updated_at",
}

var declared = []Table{
	{Name: "agent_pull_requests"},
	{Name: "agent_runs"},
	{Name: "agent_screens"},
	{Name: "agent_transcript_chunks"},
	{Name: "agent_transcript_sessions"},
	{Name: "agents", Triggers: []Trigger{setUpdatedAt}},
	{Name: "document_type_versions"},
	{Name: "document_types", Triggers: []Trigger{setUpdatedAt}},
	{Name: "document_versions"},
	{Name: "documents", Triggers: []Trigger{setUpdatedAt}},
	{Name: "engine_logins"},
	{Name: "engines"},
	{Name: "forge_connections"},
	{Name: "forge_review_comments", Triggers: []Trigger{setUpdatedAt}},
	{Name: "forge_review_threads", Triggers: []Trigger{setUpdatedAt}},
	{Name: "mail", Triggers: []Trigger{setUpdatedAt}},
	{Name: "pull_request_check_failure_notes"},
	{Name: "pull_request_check_readings"},
	{Name: "pull_request_check_steps"},
	{Name: "pull_request_checks"},
	{Name: "pull_request_syncs"},
	{Name: "pull_requests"},
	{Name: "review_forge_replies", Triggers: []Trigger{setUpdatedAt}},
	{Name: "review_messages", Triggers: []Trigger{setUpdatedAt}},
	{Name: "review_rounds"},
	{Name: "review_threads", Triggers: []Trigger{setUpdatedAt}},
	{Name: "review_viewed_files"},
	{Name: "reviews", Triggers: []Trigger{setUpdatedAt}},
	{Name: "schedule_runs"},
	{Name: "schedules", Triggers: []Trigger{setUpdatedAt}},
	{Name: "workflow_configs", Triggers: []Trigger{setUpdatedAt}},
	{Name: "workflow_event_prompts"},
	{Name: "workflow_hooks"},
	{Name: "workflow_provisioner_configs", Triggers: []Trigger{setUpdatedAt}},
	{Name: "workflows", Triggers: []Trigger{setUpdatedAt}},
	{Name: "worktrees"},
}

var functions = []Function{
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
func tables() []Table {
	tables := xslice.Map(declared, func(table Table) Table {
		return Table{Name: table.Name, Triggers: slices.Clone(table.Triggers)}
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
			tables = append(tables, Table{Name: trigger.Table})
		}
		tables[i].Triggers = append(tables[i].Triggers, Trigger{
			Name:     trigger.Name,
			Timing:   "AFTER",
			Events:   "INSERT OR UPDATE OR DELETE",
			Function: trigger.Function,
			Args:     trigger.Args,
		})
	}

	for i := range tables {
		slices.SortFunc(tables[i].Triggers, func(a, b Trigger) int { return cmp.Compare(a.Name, b.Name) })
	}
	slices.SortFunc(tables, func(a, b Table) int { return cmp.Compare(a.Name, b.Name) })

	return tables
}

// Functions lists every function a trigger runs, sorted by name.
func Functions() []Function {
	all := append(slices.Clone(functions), xslice.Map(watch.TriggerFunctions, func(function watch.TriggerFunction) Function {
		return Function{Name: function.Name, Body: function.Body}
	})...)
	slices.SortFunc(all, func(a, b Function) int { return cmp.Compare(a.Name, b.Name) })

	return all
}
