package pullrequests

import (
	"fmt"
	"testing"
	"time"

	"komrade/internal/git"
	pullrequeststore "komrade/internal/store/pullrequest"
	"komrade/internal/storetest"
	"komrade/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListPullRequests pins that a repository is read under its canonical address.
func TestListPullRequests(t *testing.T) {
	st := storetest.Open(t)
	lister := NewLister(NewListerParams{PullRequests: st.PullRequests})

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for _, pullRequest := range []struct {
		repository string
		number     int
		branch     string
	}{
		{
			repository: syncRepository,
			number:     1,
			branch:     "agent/x",
		},
		{
			repository: syncRepository,
			number:     2,
			branch:     "agent/y",
		},
		{
			repository: otherRepository,
			number:     3,
			branch:     "agent/x",
		},
	} {
		_, err := st.PullRequests.Upsert(t.Context(), pullrequeststore.UpsertParams{
			Host:              "github.com",
			Forge:             "github",
			ProviderID:        pullRequest.repository + pullRequest.branch,
			URL:               fmt.Sprintf("%s/pull/%d", pullRequest.repository, pullRequest.number),
			RepositoryURL:     pullRequest.repository,
			HeadRepositoryURL: pullRequest.repository,
			Number:            pullRequest.number,
			HeadBranch:        pullRequest.branch,
			BaseBranch:        "main",
			State:             "open",
			OpenedAt:          opened,
			ChangedAt:         opened,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name        string
		params      ListPullRequestsParams
		wantErr     error
		wantNumbers []int
	}{
		{
			name: "a remote that names no forge is refused",
			params: ListPullRequestsParams{
				RepositoryURL: "/local/path",
				Limit:         20,
			},
			wantErr: git.ErrNotForgeRemote,
		},
		{
			name: "a repository spelled as an ssh remote lists its own pull requests on the branch",
			params: ListPullRequestsParams{
				RepositoryURL: "git@github.com:harrisoncramer/komrade.git",
				HeadBranch:    "agent/x",
				Limit:         20,
			},
			wantNumbers: []int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed, err := lister.ListPullRequests(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantNumbers, xslice.Map(listed, func(pullRequest pullrequeststore.PullRequest) int {
				return pullRequest.Number
			}))
		})
	}
}
