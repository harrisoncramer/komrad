package github

import (
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetDraft covers converting a pull request each way and the forge's refusals.
func TestSetDraft(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	open := answer{body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"state": "OPEN",
			"isDraft": false
		}
	}
}`)}
	drafted := answer{body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"state": "OPEN",
			"isDraft": true
		}
	}
}`)}

	tests := []struct {
		name          string
		draft         bool
		answers       map[string]answer
		wantErr       error
		wantOperation string
	}{
		{
			name:  "an open pull request is converted to a draft",
			draft: true,
			answers: map[string]answer{"DraftTarget": open, "ConvertToDraft": {body: data(`{
	"convertPullRequestToDraft": {
		"clientMutationId": null
	}
}`)}},
			wantOperation: "ConvertToDraft",
		},
		{
			name: "a draft is marked ready for review",
			answers: map[string]answer{"DraftTarget": drafted, "MarkReady": {body: data(`{
	"markPullRequestReadyForReview": {
		"clientMutationId": null
	}
}`)}},
			wantOperation: "MarkReady",
		},
		{
			name:          "a draft asked to be a draft is left alone",
			draft:         true,
			answers:       map[string]answer{"DraftTarget": drafted},
			wantOperation: "DraftTarget",
		},
		{
			name:  "a merged pull request is refused before github is asked to change it",
			draft: true,
			answers: map[string]answer{"DraftTarget": {body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"state": "MERGED",
			"isDraft": false
		}
	}
}`)}},
			wantErr: forge.ErrNotDraftable,
		},
		{
			name:  "a pull request github will not convert",
			draft: true,
			answers: map[string]answer{
				"DraftTarget": open,
				"ConvertToDraft": {body: `{
	"data": {
		"convertPullRequestToDraft": null
	},
	"errors": [
		{
			"type": "UNPROCESSABLE",
			"path": [
				"convertPullRequestToDraft"
			],
			"message": "Draft pull requests are not supported in this repository"
		}
	]
}`},
			},
			wantErr: forge.ErrNotDraftable,
		},
		{
			name:  "a credential github will not let change the pull request",
			draft: true,
			answers: map[string]answer{
				"DraftTarget": open,
				"ConvertToDraft": {body: `{
	"data": {
		"convertPullRequestToDraft": null
	},
	"errors": [
		{
			"type": "FORBIDDEN",
			"path": [
				"convertPullRequestToDraft"
			],
			"message": "Resource not accessible by integration"
		}
	]
}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name: "a pull request github does not have",
			answers: map[string]answer{"DraftTarget": {body: data(`{
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

			err := f.SetDraft(t.Context(), address, tt.draft)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOperation, fake.last(t).Operation)
		})
	}
}
