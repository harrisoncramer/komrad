package server

import (
	"testing"

	"komrade/internal/watch"
	"komrade/internal/watched"
	"komrade/lib/xslice"
	watchv1 "komrade/proto/gen/watch/v1"

	agentstore "komrade/internal/store/agent"
	mailstore "komrade/internal/store/mail"
	markstore "komrade/internal/store/mark"
	pullrequeststore "komrade/internal/store/pullrequest"
	rowstore "komrade/internal/store/row"
	schedulestore "komrade/internal/store/schedule"
	workflowstore "komrade/internal/store/workflow"

	"github.com/stretchr/testify/assert"
)

// TestEveryWatchedResourceHasOneLoader pins one loader per declared resource and nothing else.
func TestEveryWatchedResourceHasOneLoader(t *testing.T) {
	declared := xslice.Map(watched.All(), func(resource watch.Resource) watchv1.Resource { return resource.Enum })
	loaded := xslice.Map(buildWatchLoaders(&stores{}), func(loader watch.Loader) watchv1.Resource { return loader.Resource })

	assert.ElementsMatch(t, declared, loaded)
}

// TestEveryWatchedRendererKeysAndWrapsItsRow pins that each renderer answers the row's key and a Row arm to carry it in.
func TestEveryWatchedRendererKeysAndWrapsItsRow(t *testing.T) {
	tests := []struct {
		name   string
		render func() (string, *watchv1.Row)
	}{
		{
			name: "agent",
			render: func() (string, *watchv1.Row) {
				return renderWatchedAgent(rowstore.Row{Agent: agentstore.Agent{ID: "k"}})
			},
		},
		{
			name: "workflow",
			render: func() (string, *watchv1.Row) {
				return renderWatchedWorkflow(workflowstore.Workflow{ID: "k"})
			},
		},
		{
			name: "schedule",
			render: func() (string, *watchv1.Row) {
				return renderWatchedSchedule(schedulestore.Schedule{ID: "k"})
			},
		},
		{
			name: "mark",
			render: func() (string, *watchv1.Row) {
				return renderWatchedMark(markstore.Mark{ID: "k"})
			},
		},
		{
			name: "mail",
			render: func() (string, *watchv1.Row) {
				return renderWatchedMail(mailstore.Mail{ID: "k"})
			},
		},
		{
			name: "pull request",
			render: func() (string, *watchv1.Row) {
				return renderWatchedPullRequest(pullrequeststore.PullRequest{ID: "k"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, row := tt.render()

			assert.Equal(t, "k", key)
			assert.NotNil(t, row.GetRow())
		})
	}
}
