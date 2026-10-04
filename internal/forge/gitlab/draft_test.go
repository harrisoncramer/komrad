package gitlab

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetDraft covers marking a merge request each way, leaving one alone that already stands
// that way, and refusing a finished one before GitLab is asked to change it.
func TestSetDraft(t *testing.T) {
	t.Parallel()

	const address = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/7"

	read := func(node string) answer {
		return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": ` + node + `
		}
	}
}`}
	}
	changed := answer{body: `{
	"data": {
		"mergeRequestSetDraft": {
			"errors": []
		}
	}
}`}

	tests := []struct {
		name        string
		draft       bool
		answers     map[string]answer
		wantErr     error
		wantChanged bool
	}{
		{
			name:        "an open merge request is marked a draft",
			draft:       true,
			answers:     map[string]answer{"Get": read(openMergeRequest), "SetDraft": changed},
			wantChanged: true,
		},
		{
			name:        "a draft is marked ready",
			answers:     map[string]answer{"Get": read(draftMergeRequest), "SetDraft": changed},
			wantChanged: true,
		},
		{
			name:    "a draft asked to be a draft is left alone",
			draft:   true,
			answers: map[string]answer{"Get": read(draftMergeRequest)},
		},
		{
			name:    "a merged merge request is refused",
			draft:   true,
			answers: map[string]answer{"Get": read(mergedMergeRequest)},
			wantErr: forge.ErrNotDraftable,
		},
		{
			name:    "a closed merge request is refused",
			answers: map[string]answer{"Get": read(forkMergeRequest)},
			wantErr: forge.ErrNotDraftable,
		},
		{
			name:  "a merge request gitlab will not change",
			draft: true,
			answers: map[string]answer{
				"Get": read(openMergeRequest),
				"SetDraft": {body: `{
	"data": {
		"mergeRequestSetDraft": {
			"errors": [
				"You don't have permission to update this merge request"
			]
		}
	}
}`},
			},
			wantErr:     forge.ErrNotDraftable,
			wantChanged: true,
		},
		{
			name:    "a merge request gitlab does not have",
			answers: map[string]answer{"Get": read("null")},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetDraft(t.Context(), address, tt.draft)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			sent := xslice.Filter(fake.calls, func(c call) bool { return c.Operation == "SetDraft" })
			if !tt.wantChanged {
				assert.Empty(t, sent)

				return
			}
			require.Len(t, sent, 1)
			assert.Equal(t, map[string]any{"path": "harrisoncramer/komrad", "iid": "7", "draft": tt.draft}, sent[0].Variables)
		})
	}
}
