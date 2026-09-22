package watch

//go:generate go run gen/main.go

import (
	"strings"

	"weave/internal/rpc/render"
	watchv1 "weave/proto/gen/watch/v1"

	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Resource is one table the watch carries the rows of.
type Resource struct {
	// Table is the Postgres table the rows are read from.
	Table string
	// Key is the column the row is keyed by, which a notification names it under.
	Key string
	// Enum is the value the wire sends the row under.
	Enum watchv1.Resource
	// Message is the proto message one row is drawn as.
	Message protoreflect.MessageDescriptor
	// Projects is the tables rendered onto this one, whose writes move it.
	Projects []Projection

	arm arm
}

// Plural is the table's name in camel case.
func (r Resource) Plural() string {
	return camel(r.Table)
}

// Projection is one table rendered onto a resource, and how a row of it finds the resource it
// belongs to.
type Projection struct {
	// Table is the projected table.
	Table string
	// Column is the column on the projected row holding the resource's key.
	Column string
	// Lookup is the column on the resource's table matched against Via, when the key is not on
	// the projected row.
	Lookup string
	// Via is the column on the projected row that Lookup is matched against.
	Via string
	// Where narrows the lookup, such as archived_at IS NULL.
	Where string
}

// resourcesByTable is the resource each table a notification names is the rows of.
var resourcesByTable = func() map[string]watchv1.Resource {
	byTable := map[string]watchv1.Resource{}
	for _, r := range Resources() {
		byTable[r.Table] = r.Enum
	}

	return byTable
}()

// Resources is every table the watch carries the rows of, which is what the trigger generator
// reads.
func Resources() []Resource {
	return resources(NewRowsParams{})
}

// resources is the one declaration of what the watch carries: the table each resource is the
// rows of, how a row is read back and drawn, and the tables whose writes move it.
func resources(params NewRowsParams) []Resource {
	return []Resource{
		newResource(newResourceParams[rowstore.Row, *agentv1.Agent]{
			Enum:   watchv1.Resource_RESOURCE_AGENT,
			Table:  "agents",
			Key:    "id",
			Store:  params.Agents,
			RowKey: func(row rowstore.Row) string { return row.ID },
			Draw:   func(row rowstore.Row) *agentv1.Agent { return render.Agent(row, render.AgentOptions{}) },
			Wrap:   func(row *agentv1.Agent) *watchv1.Row { return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: row}} },
			Projects: []Projection{
				{Table: "agent_metadata", Column: "agent_id"},
				{Table: "agent_runs", Column: "agent_id"},
				{Table: "reviews", Column: "agent_id"},
				{Table: "review_rounds", Column: "agent_id"},
				{Table: "pull_request_syncs", Lookup: "repository_url", Via: "repository_url", Where: "archived_at IS NULL"},
			},
		}),
		newResource(newResourceParams[workflowstore.Workflow, *workflowv1.Workflow]{
			Enum:   watchv1.Resource_RESOURCE_WORKFLOW,
			Table:  "workflows",
			Key:    "id",
			Store:  params.Workflows,
			RowKey: func(row workflowstore.Workflow) string { return row.ID },
			Draw: func(row workflowstore.Workflow) *workflowv1.Workflow {
				return render.Workflow(row, render.WorkflowOptions{})
			},
			Wrap: func(row *workflowv1.Workflow) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: row}}
			},
			Projects: []Projection{
				{Table: "workflow_hooks", Column: "workflow_id"},
				{Table: "workflow_configs", Lookup: "workflow_config_id", Via: "id"},
				{Table: "workflow_provisioner_configs", Lookup: "provisioner_config_id", Via: "id"},
			},
		}),
		newResource(newResourceParams[schedulestore.Schedule, *schedulev1.Schedule]{
			Enum:   watchv1.Resource_RESOURCE_SCHEDULE,
			Table:  "schedules",
			Key:    "id",
			Store:  params.Schedules,
			RowKey: func(row schedulestore.Schedule) string { return row.ID },
			Draw: func(row schedulestore.Schedule) *schedulev1.Schedule {
				return render.Schedule(row, params.now(), render.ScheduleOptions{})
			},
			Wrap: func(row *schedulev1.Schedule) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: row}}
			},
		}),
		newResource(newResourceParams[markstore.Mark, *agentv1.Mark]{
			Enum:   watchv1.Resource_RESOURCE_MARK,
			Table:  "marks",
			Key:    "mark",
			Store:  params.Marks,
			RowKey: func(row markstore.Mark) string { return row.Mark },
			Draw:   render.Mark,
			Wrap:   func(row *agentv1.Mark) *watchv1.Row { return &watchv1.Row{Row: &watchv1.Row_Mark{Mark: row}} },
		}),
		newResource(newResourceParams[mailstore.Mail, *agentv1.Mail]{
			Enum:   watchv1.Resource_RESOURCE_MAIL,
			Table:  "mail",
			Key:    "id",
			Store:  params.Mail,
			RowKey: func(row mailstore.Mail) string { return row.ID },
			Draw:   render.Mail,
			Wrap:   func(row *agentv1.Mail) *watchv1.Row { return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: row}} },
		}),
		newResource(newResourceParams[pullrequeststore.PullRequest, *pullrequestv1.PullRequest]{
			Enum:   watchv1.Resource_RESOURCE_PULL_REQUEST,
			Table:  "pull_requests",
			Key:    "id",
			Store:  params.PullRequests,
			RowKey: func(row pullrequeststore.PullRequest) string { return row.ID },
			Draw:   render.PullRequest,
			Wrap: func(row *pullrequestv1.PullRequest) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: row}}
			},
		}),
	}
}

// newResourceParams declares one resource: R is the store row it is read as and M the proto
// message it is drawn as.
type newResourceParams[R any, M proto.Message] struct {
	// Enum is the value the wire sends the row under.
	Enum watchv1.Resource
	// Table is the Postgres table the rows are read from.
	Table string
	// Key is the column the row is keyed by.
	Key string
	// Store reads the rows, every one for a snapshot and by key for a read-back.
	Store reader[R]
	// RowKey is the key column's value on a stored row.
	RowKey func(R) string
	// Draw renders one stored row the way the list RPCs do.
	Draw func(R) M
	// Wrap puts a drawn row in the Row arm this resource fills.
	Wrap func(M) *watchv1.Row
	// Projects is the tables rendered onto this one, whose writes move it.
	Projects []Projection
}

// newResource is one entry of the list, which does not compile without its store, its key, its
// renderer and its Row arm.
func newResource[R any, M proto.Message](params newResourceParams[R, M]) Resource {
	var message M

	return Resource{
		Table:    params.Table,
		Key:      params.Key,
		Enum:     params.Enum,
		Message:  message.ProtoReflect().Descriptor(),
		Projects: params.Projects,
		arm:      newArm(params.Store, params.RowKey, func(row R) *watchv1.Row { return params.Wrap(params.Draw(row)) }),
	}
}

// camel turns pull_requests into PullRequests.
func camel(table string) string {
	var b strings.Builder
	for word := range strings.SplitSeq(table, "_") {
		if word == "" {
			continue
		}

		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}

	return b.String()
}
