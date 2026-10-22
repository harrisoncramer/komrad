package paneltest

import (
	"fmt"

	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	taskv1 "komrad/proto/gen/task/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"google.golang.org/protobuf/proto"
)

// Snapshot is a watch event that replaces everything the panel holds with rows.
func Snapshot(rows ...proto.Message) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: xslice.Map(rows, wrapRow)}}}
}

// Puts is a watch event that adds or replaces rows.
func Puts(rows ...proto.Message) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Puts: xslice.Map(rows, wrapRow)}}}
}

// Deleted is a watch event that removes one row by its key.
func Deleted(resource watchv1.Resource, key string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Deleted: []*watchv1.Deleted{{
		Resource: resource,
		Key:      key,
	}}}}}
}

func wrapRow(row proto.Message) *watchv1.Row {
	switch row := row.(type) {
	case *agentv1.Agent:
		return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: row}}
	case *workflowv1.Workflow:
		return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: row}}
	case *schedulev1.Schedule:
		return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: row}}
	case *agentv1.WatchedReview:
		return &watchv1.Row{Row: &watchv1.Row_Review{Review: row}}
	case *agentv1.ReviewThread:
		return &watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: row}}
	case *agentv1.Mail:
		return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: row}}
	case *pullrequestv1.PullRequest:
		return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: row}}
	case *pullrequestv1.Repository:
		return &watchv1.Row{Row: &watchv1.Row_Repository{Repository: row}}
	case *worktreev1.WatchedWorktree:
		return &watchv1.Row{Row: &watchv1.Row_Worktree{Worktree: row}}
	case *agentv1.DocumentType:
		return &watchv1.Row{Row: &watchv1.Row_DocumentType{DocumentType: row}}
	case *taskv1.Task:
		return &watchv1.Row{Row: &watchv1.Row_Task{Task: row}}
	default:
		panic(fmt.Sprintf("no watch row holds a %T", row))
	}
}
