package agents

import (
	"strings"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// pullRequestFields are the record's rows for the branch's pull requests: the current one and its
// author, then the rest, all of them when the branch has moved past its last one.
func pullRequestFields(standing panel.PullRequestStanding, pullRequests []panel.PullRequest) []record.Field {
	if len(pullRequests) == 0 {
		return nil
	}

	var fields []record.Field
	rest := pullRequests
	if standing.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		current := pullRequests[0]
		rest = pullRequests[1:]
		if current.Title != "" {
			fields = append(fields, record.Field{Label: current.Noun(), Value: current.Ref() + " " + current.Title})
		}
		if current.Author != "" {
			fields = append(fields, record.Field{Label: "author", Value: "@" + current.Author})
		}
	}
	if len(rest) == 0 {
		return fields
	}

	words := xslice.Map(rest, func(pullRequest panel.PullRequest) string {
		return pullRequest.Ref() + " " + pullRequest.Words()
	})

	return append(fields, record.Field{Label: rest[0].Noun() + "s", Value: strings.Join(words, ", ")})
}
