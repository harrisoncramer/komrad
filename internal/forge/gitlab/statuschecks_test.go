package gitlab

import (
	"cmp"
	"encoding/json"
	"testing"

	"komrad/internal/forge"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

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
		wantState      pullrequestv1.StatusCheckState
		wantConclusion string
	}{
		{
			status:         pipelineSuccess,
			wantState:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
			wantConclusion: "success",
		},
		{
			status:         pipelineFailed,
			wantState:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
			wantConclusion: "failed",
		},
		{status: pipelineCreated, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED},
		{status: pipelineWaitingForResource, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED},
		{status: pipelinePreparing, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED},
		{status: pipelineWaitingForCallback, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING},
		{status: pipelinePending, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED},
		{status: pipelineRunning, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING},
		{status: pipelineScheduled, wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED},
		{
			status:         pipelineCanceled,
			wantState:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
			wantConclusion: "canceled",
		},
		{
			status:         pipelineCanceling,
			wantState:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
			wantConclusion: "canceling",
		},
		{status: pipelineSkipped},
		{status: pipelineManual},
		{
			status:         pipelineFailed,
			eventType:      pipelineEventDetached,
			wantState:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
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
			wantState:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
			wantConclusion: "success",
		},
		{
			status:    pipelineRunning,
			sha:       "def",
			eventType: pipelineEventMergeTrain,
			wantState: pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
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
			if tt.wantState != pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNSPECIFIED {
				want.StatusChecks = []forge.StatusCheck{{
					ProviderID: "7",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE,
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
