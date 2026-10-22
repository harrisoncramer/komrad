package github

import (
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetBase covers re-targeting a pull request and the forge's refusals.
func TestSetBase(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	onParent := answer{body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"state": "OPEN",
			"baseRefName": "task/parent"
		}
	}
}`)}

	tests := []struct {
		name          string
		answers       map[string]answer
		wantErr       error
		wantOperation string
	}{
		{
			name: "an open pull request is pointed at the base branch",
			answers: map[string]answer{"BaseTarget": onParent, "SetBase": {body: data(`{
	"updatePullRequest": {
		"clientMutationId": null
	}
}`)}},
			wantOperation: "SetBase",
		},
		{
			name: "a pull request already on the base branch is left alone",
			answers: map[string]answer{"BaseTarget": {body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"state": "OPEN",
			"baseRefName": "main"
		}
	}
}`)}},
			wantOperation: "BaseTarget",
		},
		{
			name: "a merged pull request is refused before github is asked to change it",
			answers: map[string]answer{"BaseTarget": {body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"state": "MERGED",
			"baseRefName": "task/parent"
		}
	}
}`)}},
			wantErr: forge.ErrNotRetargetable,
		},
		{
			name: "a base branch github will not take",
			answers: map[string]answer{
				"BaseTarget": onParent,
				"SetBase": {body: `{
	"data": {
		"updatePullRequest": null
	},
	"errors": [
		{
			"type": "UNPROCESSABLE",
			"path": [
				"updatePullRequest"
			],
			"message": "Proposed base branch was not found"
		}
	]
}`},
			},
			wantErr: forge.ErrNotRetargetable,
		},
		{
			name: "a pull request github does not have",
			answers: map[string]answer{"BaseTarget": {body: data(`{
	"repository": {
		"pullRequest": null
	}
}`)}},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetBase(t.Context(), address, "main")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOperation, fake.last(t).Operation)
		})
	}
}
