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

var (
	setUpdatedAt = Trigger{Name: "set_updated_at", Timing: "BEFORE", Events: "UPDATE", Function: "set_updated_at"}
	stampEngine  = Trigger{Name: "stamp_engine", Timing: "BEFORE", Events: "INSERT", Function: "stamp_engine"}
)

var declared = []Table{
	{Name: "agent_pull_requests", Triggers: []Trigger{stampEngine}},
	{Name: "agent_runs", Triggers: []Trigger{stampEngine}},
	{Name: "agent_screens", Triggers: []Trigger{stampEngine}},
	{Name: "agent_transcript_chunks", Triggers: []Trigger{stampEngine}},
	{Name: "agent_transcript_sessions", Triggers: []Trigger{stampEngine}},
	{Name: "agents", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "document_type_versions", Triggers: []Trigger{stampEngine}},
	{Name: "document_types", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "document_versions", Triggers: []Trigger{stampEngine}},
	{Name: "documents", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "engine_logins"},
	{Name: "engines"},
	{Name: "forge_connections", Triggers: []Trigger{stampEngine}},
	{Name: "forge_review_comments", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "forge_review_threads", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "mail", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "pull_request_syncs", Triggers: []Trigger{stampEngine}},
	{Name: "pull_requests", Triggers: []Trigger{stampEngine}},
	{Name: "review_messages", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "review_rounds", Triggers: []Trigger{stampEngine}},
	{Name: "review_threads", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "review_viewed_files", Triggers: []Trigger{stampEngine}},
	{Name: "reviews", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "schedule_runs", Triggers: []Trigger{stampEngine}},
	{Name: "schedules", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "workflow_configs", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "workflow_event_prompts", Triggers: []Trigger{stampEngine}},
	{Name: "workflow_hooks", Triggers: []Trigger{stampEngine}},
	{Name: "workflow_provisioner_configs", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "workflows", Triggers: []Trigger{stampEngine, setUpdatedAt}},
	{Name: "worktrees", Triggers: []Trigger{stampEngine}},
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
	{
		Name: "stamp_engine",
		Body: `
BEGIN
    NEW.engine_id := COALESCE(current_engine(), NEW.engine_id);
    RETURN NEW;
END;
`,
	},
}

// Tables lists every table in the schema with the triggers it carries, each table's sorted by name.
func Tables() []Table {
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
