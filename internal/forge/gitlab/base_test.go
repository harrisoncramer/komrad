package gitlab

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetBase covers re-targeting a merge request, leaving one alone that already targets the
// branch, and refusing a finished one before asking GitLab to change it.
func TestSetBase(t *testing.T) {
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
		"mergeRequestUpdate": {
			"errors": []
		}
	}
}`}

	tests := []struct {
		name        string
		base        string
		answers     map[string]answer
		wantErr     error
		wantChanged bool
	}{
		{
			name:        "an open merge request is pointed at another branch",
			base:        "main",
			answers:     map[string]answer{"Get": read(openMergeRequest), "SetBase": changed},
			wantChanged: true,
		},
		{
			name:    "a merge request already on the branch is left alone",
			base:    "master",
			answers: map[string]answer{"Get": read(openMergeRequest)},
		},
		{
			name:    "a merged merge request is refused",
			base:    "main",
			answers: map[string]answer{"Get": read(mergedMergeRequest)},
			wantErr: forge.ErrNotRetargetable,
		},
		{
			name: "a merge request gitlab will not change",
			base: "main",
			answers: map[string]answer{
				"Get": read(openMergeRequest),
				"SetBase": {body: `{
	"data": {
		"mergeRequestUpdate": {
			"errors": [
				"Target branch main does not exist"
			]
		}
	}
}`},
			},
			wantErr:     forge.ErrNotRetargetable,
			wantChanged: true,
		},
		{
			name:    "a merge request gitlab does not have",
			base:    "main",
			answers: map[string]answer{"Get": read("null")},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetBase(t.Context(), address, tt.base)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			sent := xslice.Filter(fake.calls, func(c call) bool { return c.Operation == "SetBase" })
			if !tt.wantChanged {
				assert.Empty(t, sent)

				return
			}
			require.Len(t, sent, 1)
			assert.Equal(t, map[string]any{"path": "harrisoncramer/komrad", "iid": "7", "base": tt.base}, sent[0].Variables)
		})
	}
}
