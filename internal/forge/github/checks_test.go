package github

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadBranchesCheckListing covers what an open pull request's listing holds when GitHub's
// answer for its head is missing, for another commit, or blank.
func TestReadBranchesCheckListing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		checks string
		want   *forge.CheckListing
	}{
		{
			name:   "a pull request github no longer finds lists nothing",
			checks: data(`{"p0":null}`),
		},
		{
			name:   "an answer for a commit pushed since the read lists nothing",
			checks: data(`{"p0":{"commits":{"nodes":[{"commit":{"oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","statusCheckRollup":{"contexts":{"nodes":[{"__typename":"CheckRun","id":"CR_1","name":"test","status":"IN_PROGRESS"}]}}}}]}}}`),
		},
		{
			name:   "a head with no checks yet is a blank listing",
			checks: data(`{"p0":{"commits":{"nodes":[{"commit":{"oid":"acc6d4fd6caf3511c998a673406e6fed2df7a28b","statusCheckRollup":null}}]}}}`),
			want:   &forge.CheckListing{},
		},
		{
			name:   "checks in flight are queued or running and checks that cannot start alone are waiting",
			checks: data(`{"p0":{"commits":{"nodes":[{"commit":{"oid":"acc6d4fd6caf3511c998a673406e6fed2df7a28b","statusCheckRollup":{"contexts":{"nodes":[{"__typename":"CheckRun","id":"CR_1","name":"test","status":"IN_PROGRESS","startedAt":"2026-09-20T11:00:00Z"},{"__typename":"StatusContext","id":"SC_1","context":"ci/legacy","state":"PENDING","createdAt":"2026-09-20T11:01:00Z"},{"__typename":"StatusContext","id":"SC_2","context":"ci/deploy","state":"EXPECTED"},{"__typename":"CheckRun","id":"CR_2","name":"deploy","status":"WAITING"},{"__typename":"CheckRun","id":"CR_3","name":"lint","status":"PENDING"}]}}}}]}}}`),
			want: &forge.CheckListing{Checks: []forge.Check{
				{ProviderID: "CR_1", Kind: forge.CheckKindRun, Name: "test", State: forge.CheckStateRunning, StartedAt: xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC))},
				{ProviderID: "SC_1", Kind: forge.CheckKindStatus, Name: "ci/legacy", State: forge.CheckStateRunning, StartedAt: xptr.Ptr(time.Date(2026, 9, 20, 11, 1, 0, 0, time.UTC))},
				{ProviderID: "SC_2", Kind: forge.CheckKindStatus, Name: "ci/deploy", State: forge.CheckStateWaiting},
				{ProviderID: "CR_2", Kind: forge.CheckKindRun, Name: "deploy", State: forge.CheckStateWaiting},
				{ProviderID: "CR_3", Kind: forge.CheckKindRun, Name: "lint", State: forge.CheckStateQueued},
			}},
		},
		{
			name:   "a head with more checks than one page is a truncated listing",
			checks: data(`{"p0":{"commits":{"nodes":[{"commit":{"oid":"acc6d4fd6caf3511c998a673406e6fed2df7a28b","statusCheckRollup":{"contexts":{"pageInfo":{"hasNextPage":true},"nodes":[{"__typename":"CheckRun","id":"CR_1","name":"test","status":"QUEUED"}]}}}}]}}}`),
			want:   &forge.CheckListing{Checks: []forge.Check{{ProviderID: "CR_1", Kind: forge.CheckKindRun, Name: "test", State: forge.CheckStateQueued}}, Truncated: true},
		},
		{
			name:   "a check read github refuses lists nothing and keeps the branch read",
			checks: `{"data":null,"errors":[{"message":"Something went wrong while executing your query."}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, map[string]answer{
				"ReadBranches": {body: branchesRead(withMergeReadiness(openPull, failingMergeReadiness))},
				"ReadChecks":   {body: tt.checks},
			})

			reading, err := f.ReadBranches(t.Context(), fixture, []string{"fixture/open"})
			require.NoError(t, err)
			require.Len(t, reading.PullRequests, 1)
			assert.Equal(t, tt.want, reading.PullRequests[0].MergeReadiness.Listing)
		})
	}
}
