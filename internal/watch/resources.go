package watch

import watchv1 "weave/proto/gen/watch/v1"

// Resource is one table the watch keeps track of.
type Resource struct {
	// Table is the Postgres table the rows are read from.
	Table string
	// Enum is the value the wire sends the row under.
	Enum watchv1.Resource
	// Subresources includes all of the subtables that are  for a given resource.
	Subresources []Subresource
}

// Subresource is one table that is rendered or "projected" onto another resource, e.g. agent_metadata into agents.
type Subresource struct {
	// Table is the subresource table.
	Table string
	// Key is the column on the subresource row holding the resource's id.
	Key string
	// Lookup finds the resources a subresource row belongs to when the row does not hold their id.
	Lookup *Lookup
}

// Lookup matches a subresource row to its resources by a column on each side.
type Lookup struct {
	// ParentColumn is the column on the resource's table.
	ParentColumn string
	// ChildColumn is the column on the subresource row that ParentColumn is matched against.
	ChildColumn string
	// SkipParentsWith is a column of the resource's table that has to be null for the resource to be notified.
	SkipParentsWith string
}

// resources is every table the watch carries the rows of.
var resources = []Resource{
	{
		Enum:  watchv1.Resource_RESOURCE_AGENT,
		Table: "agents",
		Subresources: []Subresource{
			{
				Table: "agent_metadata",
				Key:   "agent_id",
			},
			{
				Table: "agent_runs",
				Key:   "agent_id",
			},
			{
				Table: "reviews",
				Key:   "agent_id",
			},
			{
				Table: "review_rounds",
				Key:   "agent_id",
			},
			{
				Table: "review_threads",
				Key:   "agent_id",
			},
			{
				Table: "pull_request_syncs",
				Lookup: &Lookup{
					ParentColumn:    "repository_url",
					ChildColumn:     "repository_url",
					SkipParentsWith: "archived_at",
				},
			},
		},
	},
	{
		Enum:  watchv1.Resource_RESOURCE_WORKFLOW,
		Table: "workflows",
		Subresources: []Subresource{
			{
				Table: "workflow_hooks",
				Key:   "workflow_id",
			},
			{
				Table: "workflow_configs",
				Lookup: &Lookup{
					ParentColumn: "workflow_config_id",
					ChildColumn:  "id",
				},
			},
			{
				Table: "workflow_provisioner_configs",
				Lookup: &Lookup{
					ParentColumn: "provisioner_config_id",
					ChildColumn:  "id",
				},
			},
		},
	},
	{
		Enum:  watchv1.Resource_RESOURCE_SCHEDULE,
		Table: "schedules",
	},
	{
		Enum:  watchv1.Resource_RESOURCE_MARK,
		Table: "marks",
	},
	{
		Enum:  watchv1.Resource_RESOURCE_MAIL,
		Table: "mail",
	},
	{
		Enum:  watchv1.Resource_RESOURCE_PULL_REQUEST,
		Table: "pull_requests",
	},
}

// findResource returns the resource whose rows a table holds.
func findResource(table string) (watchv1.Resource, bool) {
	for _, r := range resources {
		if r.Table == table {
			return r.Enum, true
		}
	}

	return watchv1.Resource_RESOURCE_UNSPECIFIED, false
}
