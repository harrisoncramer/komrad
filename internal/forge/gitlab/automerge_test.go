package gitlab

import (
	"net/http"
	"strconv"
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetAutoMerge covers queueing a merge request held to the head the person saw, cancelling
// it, leaving one alone that already stands that way, and refusing one GitLab would merge at once
// or that has finished before asking GitLab to change it.
func TestSetAutoMerge(t *testing.T) {
	t.Parallel()

	const (
		address = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/7"
		path    = "/projects/harrisoncramer%2Fkomrad/merge_requests/7"
	)

	target := func(state string, enabled bool, strategies string) answer {
		return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"state": "` + state + `",
				"autoMergeEnabled": ` + strconv.FormatBool(enabled) + `,
				"availableAutoMergeStrategies": ` + strategies + `
			}
		}
	}
}`}
	}
	waiting := `["merge_when_checks_pass"]`

	tests := []struct {
		name       string
		params     forge.SetAutoMergeParams
		answers    map[string]answer
		wantErr    error
		wantMethod string
		wantPath   string
		wantBody   map[string]any
	}{
		{
			name:    "a merge request gitlab does not have",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": {body: missingMergeRequest}},
			wantErr: forge.ErrNotFound,
		},
		{
			name:    "a merged merge request is refused",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": target("merged", false, "[]")},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:    "a merge request with nothing to wait for is refused rather than merged at once",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": target("opened", false, "[]")},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:   "a draft gitlab offers a strategy for is refused",
			params: forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": {body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"state": "opened",
				"draft": true,
				"autoMergeEnabled": false,
				"availableAutoMergeStrategies": [
					"merge_when_checks_pass"
				]
			}
		}
	}
}`}},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:   "a queued draft is cancelled",
			params: forge.SetAutoMergeParams{URL: address},
			answers: map[string]answer{
				"AutoMergeTarget": {body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"state": "opened",
				"draft": true,
				"autoMergeEnabled": true,
				"availableAutoMergeStrategies": [
					"merge_when_checks_pass"
				]
			}
		}
	}
}`},
				path + "/cancel_merge_when_pipeline_succeeds": {status: http.StatusCreated, body: `{"iid":7,"merge_when_pipeline_succeeds":false}`},
			},
			wantMethod: http.MethodPost,
			wantPath:   path + "/cancel_merge_when_pipeline_succeeds",
		},
		{
			name: "a merge request whose branch moved past the head the person saw",
			params: forge.SetAutoMergeParams{
				URL:     address,
				HeadSHA: "abc123",
				On:      true,
			},
			answers: map[string]answer{
				"AutoMergeTarget": target("opened", false, waiting),
				path + "/merge":   {status: http.StatusConflict, body: `{"message":"SHA does not match HEAD of source branch"}`},
			},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:    "a queued merge request asked to be queued is left alone",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": target("opened", true, waiting)},
		},
		{
			name: "an open merge request is queued at the head the person saw",
			params: forge.SetAutoMergeParams{
				URL:     address,
				HeadSHA: "abc123",
				On:      true,
			},
			answers: map[string]answer{
				"AutoMergeTarget": target("opened", false, waiting),
				path + "/merge":   {body: `{"iid":7,"state":"opened","merge_when_pipeline_succeeds":true}`},
			},
			wantMethod: http.MethodPut,
			wantPath:   path + "/merge",
			wantBody:   map[string]any{"auto_merge": true, "merge_when_pipeline_succeeds": true, "sha": "abc123"},
		},
		{
			name:   "a queued merge request is cancelled",
			params: forge.SetAutoMergeParams{URL: address},
			answers: map[string]answer{
				"AutoMergeTarget": target("opened", true, waiting),
				path + "/cancel_merge_when_pipeline_succeeds": {status: http.StatusCreated, body: `{"iid":7,"merge_when_pipeline_succeeds":false}`},
			},
			wantMethod: http.MethodPost,
			wantPath:   path + "/cancel_merge_when_pipeline_succeeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetAutoMerge(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			sent := xslice.Filter(fake.calls, func(c call) bool { return c.Operation == "" })
			if tt.wantPath == "" {
				assert.Empty(t, sent)

				return
			}
			require.Len(t, sent, 1)
			assert.Equal(t, tt.wantMethod, sent[0].Method)
			assert.Equal(t, tt.wantPath, sent[0].Path)
			assert.Equal(t, tt.wantBody, sent[0].Body)
		})
	}
}

// TestAutoMergeOnADraft covers a draft never being offered auto-merge, though GitLab offers a
// strategy, while one already queued still reads on.
func TestAutoMergeOnADraft(t *testing.T) {
	t.Parallel()

	node := mergeRequestNode{
		mergeReadinessNode: mergeReadinessNode{AutoMergeEnabled: true, AvailableAutoMergeStrategies: []string{"merge_when_checks_pass"}},
		Draft:              true,
	}
	assert.Equal(t, forge.AutoMerge{On: true, Allowed: false}, node.autoMerge())
}
