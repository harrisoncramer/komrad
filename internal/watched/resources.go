// Package watched lists every resource the watch carries.
package watched

import (
	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"
)

var agents = watch.Resource{
	Enum:           watchv1.Resource_RESOURCE_AGENT,
	Table:          "agents",
	KeyColumn:      "id",
	IgnoredColumns: []string{"state_seen_at", "updated_at"},
	Feeders: []watch.Feeder{
		{
			Table:  "agent_metadata",
			Column: "agent_id",
		},
		{
			Table:  "agent_runs",
			Column: "agent_id",
		},
		{
			Table:  "reviews",
			Column: "agent_id",
		},
		{
			Table:  "review_rounds",
			Column: "agent_id",
		},
		{
			Table:  "review_threads",
			Column: "agent_id",
		},
		{
			Table: "pull_request_syncs",
			Lookup: &watch.Lookup{
				ParentColumn:    "repository_url",
				ChildColumn:     "repository_url",
				SkipParentsWith: "archived_at",
			},
		},
	},
}

var workflows = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_WORKFLOW,
	Table:     "workflows",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table:  "workflow_hooks",
			Column: "workflow_id",
		},
		{
			Table: "workflow_configs",
			Lookup: &watch.Lookup{
				ParentColumn: "workflow_config_id",
				ChildColumn:  "id",
			},
		},
		{
			Table: "workflow_provisioner_configs",
			Lookup: &watch.Lookup{
				ParentColumn: "provisioner_config_id",
				ChildColumn:  "id",
			},
		},
	},
}

var schedules = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_SCHEDULE,
	Table:     "schedules",
	KeyColumn: "id",
}

var marks = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_MARK,
	Table:     "marks",
	KeyColumn: "id",
}

var mail = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_MAIL,
	Table:     "mail",
	KeyColumn: "id",
}

var pullRequests = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_PULL_REQUEST,
	Table:     "pull_requests",
	KeyColumn: "id",
}

// All lists every resource the watch carries the rows of.
func All() []watch.Resource {
	return []watch.Resource{agents, workflows, schedules, marks, mail, pullRequests}
}
