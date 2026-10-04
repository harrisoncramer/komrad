package gitlab

import (
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadMergeRequest covers one merge request by its web address.
func TestReadMergeRequest(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Get": {body: `{
	"data": {
		"project": {
			"mergeRequest": ` + forkMergeRequest + `
		}
	}
}`}})

	pullRequest, err := f.readMergeRequest(t.Context(), "https://gitlab.com/acme/widgets/-/merge_requests/256485")
	require.NoError(t, err)
	assert.Equal(t, 256485, pullRequest.Number)
	assert.Equal(t, "Draft: feat(reports): one template for the blockers page", pullRequest.Title)
	assert.Equal(t, map[string]any{"path": "acme/widgets", "iid": "256485"}, fake.last(t).Variables)
}

// TestReadMergeRequestRefusesWhatIsNotAMergeRequest covers the addresses readMergeRequest will not ask GitLab about.
func TestReadMergeRequestRefusesWhatIsNotAMergeRequest(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	tests := []struct {
		name    string
		address string
	}{
		{name: "a pull request path", address: "https://github.com/owner/repo/pull/1"},
		{name: "an issue", address: "https://gitlab.com/owner/repo/-/issues/1"},
		{name: "no number", address: "https://gitlab.com/owner/repo/-/merge_requests/"},
		{name: "a project", address: "https://gitlab.com/owner/repo"},
		{name: "not a url", address: "::not a url::"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := f.readMergeRequest(t.Context(), tt.address)
			require.ErrorIs(t, err, forge.ErrNotPullRequest)
			assert.Empty(t, fake.calls)
		})
	}
}
