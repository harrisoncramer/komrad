package render

import (
	"testing"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/stretchr/testify/assert"
)

// TestPullRequestDrawsAuthorAndBody covers the forge login and description stored on the row
// reaching the wire unchanged, with no @ added to the login, since the panel adds that.
func TestPullRequestDrawsAuthorAndBody(t *testing.T) {
	drawn := PullRequest(pullrequeststore.PullRequest{Author: "octocat", Body: "Why it changed.\n\nMore."})

	assert.Equal(t, "octocat", drawn.GetAuthor())
	assert.Equal(t, "Why it changed.\n\nMore.", drawn.GetBody())
}

// TestPullRequestDrawsAutoMerge covers a row the forge has not answered about rendering as off, and
// the forge's word rendering as stored.
func TestPullRequestDrawsAutoMerge(t *testing.T) {
	tests := []struct {
		name              string
		pullRequest       pullrequeststore.PullRequest
		wantAutoMerge     bool
		wantAutoMergeable bool
	}{
		{
			name:        "a pull request not read yet",
			pullRequest: pullrequeststore.PullRequest{},
		},
		{
			name:              "a pull request the forge would queue",
			pullRequest:       pullrequeststore.PullRequest{MergeReadiness: forge.RememberedMergeReadiness{AutoMerge: &forge.AutoMerge{Allowed: true}}},
			wantAutoMergeable: true,
		},
		{
			name:          "a pull request the forge has queued",
			pullRequest:   pullrequeststore.PullRequest{MergeReadiness: forge.RememberedMergeReadiness{AutoMerge: &forge.AutoMerge{On: true}}},
			wantAutoMerge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drawn := PullRequest(tt.pullRequest)

			assert.Equal(t, tt.wantAutoMerge, drawn.GetMergeReadiness().GetAutoMerge())
			assert.Equal(t, tt.wantAutoMergeable, drawn.GetMergeReadiness().GetAutoMergeable())
		})
	}
}
