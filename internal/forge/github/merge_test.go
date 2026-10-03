package github

import (
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMerge covers merging with the repository's default method and GitHub's refusals.
func TestMerge(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	target := answer{body: data(`{"repository":{"viewerDefaultMergeMethod":"SQUASH","pullRequest":{"id":"PR_742","state":"OPEN"}}}`)}

	tests := []struct {
		name    string
		answers map[string]answer
		wantErr error
	}{
		{
			name: "the pull request merges with the method the repository defaults to",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       {body: data(`{"mergePullRequest":{"clientMutationId":null}}`)},
			},
		},
		{
			name: "a pull request github will not merge",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       {body: `{"data":{"mergePullRequest":null},"errors":[{"type":"UNPROCESSABLE","path":["mergePullRequest"],"message":"Pull Request is not mergeable"}]}`},
			},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a credential github will not let merge",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       {body: `{"data":{"mergePullRequest":null},"errors":[{"type":"FORBIDDEN","path":["mergePullRequest"],"message":"Resource not accessible by integration"}]}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:    "a pull request that has already merged is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{"repository":{"viewerDefaultMergeMethod":"MERGE","pullRequest":{"id":"PR_742","state":"MERGED"}}}`)}},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name:    "a closed pull request is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{"repository":{"viewerDefaultMergeMethod":"MERGE","pullRequest":{"id":"PR_742","state":"CLOSED"}}}`)}},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a pull request github does not have",
			answers: map[string]answer{
				"MergeTarget": {body: data(`{"repository":{"viewerDefaultMergeMethod":"MERGE","pullRequest":null}}`)},
			},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.Merge(t.Context(), address)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"id": "PR_742", "method": "SQUASH"}, fake.last(t).Variables)
		})
	}
}

// TestMergeRefusesWhatIsNotAPullRequest covers the addresses Merge will not ask GitHub about.
func TestMergeRefusesWhatIsNotAPullRequest(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	tests := []struct {
		name    string
		address string
	}{
		{name: "a merge request path", address: "https://gitlab.com/owner/repo/-/merge_requests/1"},
		{name: "an issue", address: "https://github.com/owner/repo/issues/1"},
		{name: "no number", address: "https://github.com/owner/repo/pull/"},
		{name: "a repository", address: "https://github.com/owner/repo"},
		{name: "not a url", address: "::not a url::"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := f.Merge(t.Context(), tt.address)
			require.ErrorIs(t, err, forge.ErrNotPullRequest)
			assert.Empty(t, fake.calls)
		})
	}
}

// TestMergeabilityOf covers every merge state GitHub documents.
func TestMergeabilityOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   forge.Mergeability
	}{
		{status: "CLEAN", want: forge.MergeabilityClean},
		{status: "HAS_HOOKS", want: forge.MergeabilityClean},
		{status: "UNSTABLE", want: forge.MergeabilityUnstable},
		{status: "BLOCKED", want: forge.MergeabilityBlocked},
		{status: "DRAFT", want: forge.MergeabilityBlocked},
		{status: "BEHIND", want: forge.MergeabilityBehind},
		{status: "DIRTY", want: forge.MergeabilityConflicting},
		{status: "UNKNOWN", want: forge.MergeabilityUnknown},
		{status: "", want: forge.MergeabilityUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, mergeabilityOf(tt.status))
		})
	}
}
