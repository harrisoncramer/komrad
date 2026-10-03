package gitlab

import (
	"cmp"
	"encoding/json"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListing covers every pipeline status GitLab documents and the commit it ran on: a cancelled
// pipeline is a failure, a skipped or manual one says nothing about the branch, and so does one on
// another commit, save a merged-results or merge train pipeline built for the head.
func TestListing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status         string
		sha            string
		eventType      string
		wantState      forge.CheckState
		wantConclusion string
	}{
		{status: "SUCCESS", wantState: forge.CheckStateCompleted, wantConclusion: "success"},
		{status: "FAILED", wantState: forge.CheckStateCompleted, wantConclusion: "failed"},
		{status: "CREATED", wantState: forge.CheckStateQueued},
		{status: "WAITING_FOR_RESOURCE", wantState: forge.CheckStateQueued},
		{status: "PREPARING", wantState: forge.CheckStateQueued},
		{status: "WAITING_FOR_CALLBACK", wantState: forge.CheckStateRunning},
		{status: "PENDING", wantState: forge.CheckStateQueued},
		{status: "RUNNING", wantState: forge.CheckStateRunning},
		{status: "SCHEDULED", wantState: forge.CheckStateQueued},
		{status: "CANCELED", wantState: forge.CheckStateCompleted, wantConclusion: "canceled"},
		{status: "CANCELING", wantState: forge.CheckStateCompleted, wantConclusion: "canceling"},
		{status: "SKIPPED"},
		{status: "MANUAL"},
		{
			status:         "FAILED",
			eventType:      "DETACHED",
			wantState:      forge.CheckStateCompleted,
			wantConclusion: "failed",
		},
		{
			status: "FAILED",
			sha:    "def",
		},
		{
			status:         "SUCCESS",
			sha:            "def",
			eventType:      "MERGED_RESULT",
			wantState:      forge.CheckStateCompleted,
			wantConclusion: "success",
		},
		{
			status:    "RUNNING",
			sha:       "def",
			eventType: "MERGE_TRAIN",
			wantState: forge.CheckStateRunning,
		},
	}
	for _, tt := range tests {
		sha := cmp.Or(tt.sha, "abc")
		t.Run(tt.status+" "+tt.eventType+" on "+sha, func(t *testing.T) {
			t.Parallel()

			var node mergeRequestNode
			body := `{"webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/1","diffHeadSha":"abc","headPipeline":{"id":"gid://gitlab/Ci::Pipeline/7","sha":"` + sha + `","status":"` + tt.status + `","mergeRequestEventType":"` + tt.eventType + `","path":"/acme/widgets/-/pipelines/7"}}`
			require.NoError(t, json.Unmarshal([]byte(body), &node))

			want := &forge.CheckListing{}
			if tt.wantState != "" {
				want.Checks = []forge.Check{{
					ProviderID: "7",
					Kind:       forge.CheckKindPipeline,
					Name:       "pipeline",
					State:      tt.wantState,
					Conclusion: tt.wantConclusion,
					Required:   true,
					URL:        "https://gitlab.com/acme/widgets/-/pipelines/7",
				}}
			}
			assert.Equal(t, want, node.listing())
		})
	}

	var node mergeRequestNode
	require.NoError(t, json.Unmarshal([]byte(`{"headPipeline":null}`), &node))
	assert.Equal(t, &forge.CheckListing{}, node.listing())
}
