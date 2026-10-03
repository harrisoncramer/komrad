package gitlab

import (
	"cmp"
	"encoding/json"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksFolds covers every pipeline status GitLab documents and the commit it ran on: a
// cancelled pipeline is a failure, a skipped or manual one says nothing about the branch, and so
// does one on another commit, save a merged-results or merge train pipeline built for the head.
func TestChecksFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status    string
		sha       string
		eventType string
		want      forge.Checks
	}{
		{status: "SUCCESS", want: forge.ChecksPassing},
		{status: "FAILED", want: forge.ChecksFailing},
		{status: "CREATED", want: forge.ChecksPending},
		{status: "WAITING_FOR_RESOURCE", want: forge.ChecksPending},
		{status: "PREPARING", want: forge.ChecksPending},
		{status: "WAITING_FOR_CALLBACK", want: forge.ChecksPending},
		{status: "PENDING", want: forge.ChecksPending},
		{status: "RUNNING", want: forge.ChecksPending},
		{status: "SCHEDULED", want: forge.ChecksPending},
		{status: "CANCELED", want: forge.ChecksFailing},
		{status: "CANCELING", want: forge.ChecksFailing},
		{status: "SKIPPED", want: forge.ChecksNone},
		{status: "MANUAL", want: forge.ChecksNone},
		{
			status:    "FAILED",
			eventType: "DETACHED",
			want:      forge.ChecksFailing,
		},
		{
			status: "FAILED",
			sha:    "def",
			want:   forge.ChecksNone,
		},
		{
			status:    "SUCCESS",
			sha:       "def",
			eventType: "MERGED_RESULT",
			want:      forge.ChecksPassing,
		},
		{
			status:    "RUNNING",
			sha:       "def",
			eventType: "MERGE_TRAIN",
			want:      forge.ChecksPending,
		},
	}
	for _, tt := range tests {
		sha := cmp.Or(tt.sha, "abc")
		t.Run(tt.status+" "+tt.eventType+" on "+sha, func(t *testing.T) {
			t.Parallel()

			var node mergeReadinessNode
			body := `{"diffHeadSha":"abc","headPipeline":{"sha":"` + sha + `","status":"` + tt.status + `","mergeRequestEventType":"` + tt.eventType + `"}}`
			require.NoError(t, json.Unmarshal([]byte(body), &node))
			assert.Equal(t, tt.want, node.checks())
		})
	}

	var node mergeReadinessNode
	require.NoError(t, json.Unmarshal([]byte(`{"headPipeline":null}`), &node))
	assert.Equal(t, forge.ChecksNone, node.checks())
}
