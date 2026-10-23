package ui

import (
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullRequestFields covers the current pull request's title and author and the row for the
// rest, which takes every one once the branch has moved past its last.
func TestPullRequestFields(t *testing.T) {
	yesterday := time.Now().Add(-26 * time.Hour)
	current := panel.PullRequest{Number: 742, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED}

	tests := []struct {
		name         string
		state        pullrequestv1.PullRequestState
		pullRequests []panel.PullRequest
		want         []record.Field
	}{
		{
			name: "none",
		},
		{
			name:         "only the current one",
			pullRequests: []panel.PullRequest{current},
		},
		{
			name: "the current one's number, title and author",
			pullRequests: []panel.PullRequest{
				{
					Number: 742,
					Title:  "Read pull request authors",
					Author: "harrisoncramer",
				},
			},
			want: []record.Field{
				{Label: "pull request", Value: "#742 Read pull request authors"},
				{Label: "author", Value: "@harrisoncramer"},
			},
		},
		{
			name: "an empty title and author left out",
			pullRequests: []panel.PullRequest{
				{Number: 742, Author: "harrisoncramer"},
			},
			want: []record.Field{{Label: "author", Value: "@harrisoncramer"}},
		},
		{
			name: "the earlier ones folded into one row",
			pullRequests: []panel.PullRequest{
				current,
				{
					Number:   711,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
				{
					Number:   708,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
					OpenedAt: yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []record.Field{{Label: "pull requests", Value: "#711 merged, #708 closed"}},
		},
		{
			name:  "a branch moved past its last one shows no current title or author",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
			pullRequests: []panel.PullRequest{
				{
					Number:   711,
					Title:    "Read pull request authors",
					Author:   "harrisoncramer",
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []record.Field{{Label: "pull requests", Value: "#711 merged"}},
		},
		{
			name:  "a branch on GitLab names its merge requests the way GitLab does",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
			pullRequests: []panel.PullRequest{
				{
					Forge:    forgev1.Kind_KIND_GITLAB,
					Number:   711,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []record.Field{{Label: "merge requests", Value: "!711 merged"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestFields(panel.PullRequestStanding{State: tt.state}, tt.pullRequests))
		})
	}
}

// TestTaskFieldsPlaceThePullRequestsAfterTheBranch covers the rows landing under the branch
// they are about, and not at all on a task whose checkout is on no branch.
func TestTaskFieldsPlaceThePullRequestsAfterTheBranch(t *testing.T) {
	task := panel.Task{
		Slug:   "worker",
		Branch: xptr.Ptr("agent/x"),
	}
	task.PullRequests = []panel.PullRequest{
		{
			Number: 7,
			Title:  "Fix",
			Author: "octocat",
			State:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{Number: 5, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED},
	}

	labels := readLabels(taskFields(checkoutFieldsParams{task: task}, nil))

	branch := indexOf(labels, "branch")
	require.NotEqual(t, -1, branch)
	assert.Equal(t, []string{"pull request", "author", "pull requests"}, labels[branch+1:branch+4])

	task.Branch = nil
	labels = readLabels(taskFields(checkoutFieldsParams{task: task}, nil))
	for _, absent := range []string{"pull request", "author", "pull requests"} {
		assert.NotContains(t, labels, absent)
	}
}

func readLabels(fields []record.Field) []string {
	return xslice.Map(fields, func(field record.Field) string {
		return field.Label
	})
}

func indexOf(labels []string, label string) int {
	for i, candidate := range labels {
		if candidate == label {
			return i
		}
	}

	return -1
}
