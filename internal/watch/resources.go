package watch

import (
	"strings"
	"time"

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
	// Enum is the value the wire sends the row under.
	Enum watchv1.Resource
	// Message is the proto message one row is drawn as.
	Message protoreflect.MessageDescriptor
	// Projects is the tables rendered onto this one, whose writes move it.
	Projects []Projection

	bind func(Stores, func() time.Time) arm
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
	// ParentMatch is the column on the resource's table matched against Match, when the key is
	// not on the projected row.
	ParentMatch string
	// Match is the column on the projected row that ParentMatch is matched against.
	Match string
	// ParentMustBeNull is a column of the resource's table that has to be null for the row to
	// count as a parent.
	ParentMustBeNull string
}

// resourcesByTable is the resource each table a notification names is the rows of.
var resourcesByTable = func() map[string]watchv1.Resource {
	byTable := map[string]watchv1.Resource{}
	for _, r := range Resources() {
		byTable[r.Table] = r.Enum
	}

	return byTable
}()

// Resources is every table the watch carries the rows of.
func Resources() []Resource {
	return []Resource{
		newResource(newResourceParams[rowstore.Row, *agentv1.Agent]{
			Enum:  watchv1.Resource_RESOURCE_AGENT,
			Table: "agents",
			Store: func(stores Stores) reader[rowstore.Row] { return stores.Agents },
			Draw:  func(row rowstore.Row) *agentv1.Agent { return render.Agent(row, render.AgentOptions{}) },
			Wrap:  func(row *agentv1.Agent) *watchv1.Row { return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: row}} },
			Projects: []Projection{
				{Table: "agent_metadata", Column: "agent_id"},
				{Table: "agent_runs", Column: "agent_id"},
				{Table: "reviews", Column: "agent_id"},
				{Table: "review_rounds", Column: "agent_id"},
				{Table: "pull_request_syncs", ParentMatch: "repository_url", Match: "repository_url", ParentMustBeNull: "archived_at"},
			},
		}),
		newResource(newResourceParams[workflowstore.Workflow, *workflowv1.Workflow]{
			Enum:  watchv1.Resource_RESOURCE_WORKFLOW,
			Table: "workflows",
			Store: func(stores Stores) reader[workflowstore.Workflow] { return stores.Workflows },
			Draw: func(row workflowstore.Workflow) *workflowv1.Workflow {
				return render.Workflow(row, render.WorkflowOptions{})
			},
			Wrap: func(row *workflowv1.Workflow) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: row}}
			},
			Projects: []Projection{
				{Table: "workflow_hooks", Column: "workflow_id"},
				{Table: "workflow_configs", ParentMatch: "workflow_config_id", Match: "id"},
				{Table: "workflow_provisioner_configs", ParentMatch: "provisioner_config_id", Match: "id"},
			},
		}),
		newResource(newResourceParams[schedulestore.Schedule, *schedulev1.Schedule]{
			Enum:  watchv1.Resource_RESOURCE_SCHEDULE,
			Table: "schedules",
			Store: func(stores Stores) reader[schedulestore.Schedule] { return stores.Schedules },
			DrawAt: func(row schedulestore.Schedule, now time.Time) *schedulev1.Schedule {
				return render.Schedule(row, now, render.ScheduleOptions{})
			},
			Wrap: func(row *schedulev1.Schedule) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: row}}
			},
		}),
		newResource(newResourceParams[markstore.Mark, *agentv1.Mark]{
			Enum:  watchv1.Resource_RESOURCE_MARK,
			Table: "marks",
			Store: func(stores Stores) reader[markstore.Mark] { return stores.Marks },
			Draw:  render.Mark,
			Wrap:  func(row *agentv1.Mark) *watchv1.Row { return &watchv1.Row{Row: &watchv1.Row_Mark{Mark: row}} },
		}),
		newResource(newResourceParams[mailstore.Mail, *agentv1.Mail]{
			Enum:  watchv1.Resource_RESOURCE_MAIL,
			Table: "mail",
			Store: func(stores Stores) reader[mailstore.Mail] { return stores.Mail },
			Draw:  render.Mail,
			Wrap:  func(row *agentv1.Mail) *watchv1.Row { return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: row}} },
		}),
		newResource(newResourceParams[pullrequeststore.PullRequest, *pullrequestv1.PullRequest]{
			Enum:  watchv1.Resource_RESOURCE_PULL_REQUEST,
			Table: "pull_requests",
			Store: func(stores Stores) reader[pullrequeststore.PullRequest] { return stores.PullRequests },
			Draw:  render.PullRequest,
			Wrap: func(row *pullrequestv1.PullRequest) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: row}}
			},
		}),
	}
}

// newResourceParams declares one resource: R is the store row it is read as and M the proto
// message it is drawn as.
type newResourceParams[R any, M message] struct {
	// Enum is the value the wire sends the row under.
	Enum watchv1.Resource
	// Table is the Postgres table the rows are read from.
	Table string
	// Store selects the reader for this resource.
	Store func(Stores) reader[R]
	// Draw renders one stored row the way the list RPCs do.
	Draw func(R) M
	// DrawAt renders a row that depends on the clock, in place of Draw.
	DrawAt func(R, time.Time) M
	// Wrap puts a drawn row in the Row arm this resource fills.
	Wrap func(M) *watchv1.Row
	// Projects is the tables rendered onto this one, whose writes move it.
	Projects []Projection
}

// message is a drawn row, which every resource keys by its id.
type message interface {
	proto.Message
	GetId() string
}

// newResource is one entry of the list: the store its rows are read from, the renderer that
// draws them and the Row arm they fill.
func newResource[R any, M message](params newResourceParams[R, M]) Resource {
	var message M

	return Resource{
		Table:    params.Table,
		Enum:     params.Enum,
		Message:  message.ProtoReflect().Descriptor(),
		Projects: params.Projects,
		bind: func(stores Stores, now func() time.Time) arm {
			draw := params.Draw
			if params.DrawAt != nil {
				draw = func(row R) M { return params.DrawAt(row, now()) }
			}

			return newArm(params.Store(stores), func(row R) (string, *watchv1.Row) {
				drawn := draw(row)

				return drawn.GetId(), params.Wrap(drawn)
			})
		},
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
