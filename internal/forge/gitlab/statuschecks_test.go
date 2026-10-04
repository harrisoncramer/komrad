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
		wantState      forge.StatusCheckState
		wantConclusion string
	}{
		{
			status:         pipelineSuccess,
			wantState:      forge.StatusCheckStateCompleted,
			wantConclusion: "success",
		},
		{
			status:         pipelineFailed,
			wantState:      forge.StatusCheckStateCompleted,
			wantConclusion: "failed",
		},
		{status: pipelineCreated, wantState: forge.StatusCheckStateQueued},
		{status: pipelineWaitingForResource, wantState: forge.StatusCheckStateQueued},
		{status: pipelinePreparing, wantState: forge.StatusCheckStateQueued},
		{status: pipelineWaitingForCallback, wantState: forge.StatusCheckStateRunning},
		{status: pipelinePending, wantState: forge.StatusCheckStateQueued},
		{status: pipelineRunning, wantState: forge.StatusCheckStateRunning},
		{status: pipelineScheduled, wantState: forge.StatusCheckStateQueued},
		{
			status:         pipelineCanceled,
			wantState:      forge.StatusCheckStateCompleted,
			wantConclusion: "canceled",
		},
		{
			status:         pipelineCanceling,
			wantState:      forge.StatusCheckStateCompleted,
			wantConclusion: "canceling",
		},
		{status: pipelineSkipped},
		{status: pipelineManual},
		{
			status:         pipelineFailed,
			eventType:      pipelineEventDetached,
			wantState:      forge.StatusCheckStateCompleted,
			wantConclusion: "failed",
		},
		{
			status: pipelineFailed,
			sha:    "def",
		},
		{
			status:         pipelineSuccess,
			sha:            "def",
			eventType:      pipelineEventMergedResult,
			wantState:      forge.StatusCheckStateCompleted,
			wantConclusion: "success",
		},
		{
			status:    pipelineRunning,
			sha:       "def",
			eventType: pipelineEventMergeTrain,
			wantState: forge.StatusCheckStateRunning,
		},
	}
	for _, tt := range tests {
		sha := cmp.Or(tt.sha, "abc")
		t.Run(tt.status+" "+tt.eventType+" on "+sha, func(t *testing.T) {
			t.Parallel()

			var node mergeRequestNode
			body := `{
	"webUrl": "https://gitlab.com/acme/widgets/-/merge_requests/1",
	"diffHeadSha": "abc",
	"headPipeline": {
		"id": "gid://gitlab/Ci::Pipeline/7",
		"sha": "` + sha + `",
		"status": "` + tt.status + `",
		"mergeRequestEventType": "` + tt.eventType + `",
		"path": "/acme/widgets/-/pipelines/7"
	}
}`
			require.NoError(t, json.Unmarshal([]byte(body), &node))

			want := &forge.StatusCheckListing{}
			if tt.wantState != "" {
				want.StatusChecks = []forge.StatusCheck{{
					ProviderID: "7",
					Kind:       forge.StatusCheckKindPipeline,
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
	require.NoError(t, json.Unmarshal([]byte(`{
	"headPipeline": null
}`), &node))
	assert.Equal(t, &forge.StatusCheckListing{}, node.listing())
}
