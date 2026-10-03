package gitlab

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadBranches covers the query, each answer folded into komrad's words, the rate limit off
// GitLab's headers, and a fork's merge request left out.
func TestReadBranches(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ReadBranches": {
		header: map[string]string{"RateLimit-Remaining": "1999", "RateLimit-Limit": "2000", "RateLimit-Reset": "1789900000"},
		body:   readBranchesAnswer,
	}})

	reading, err := f.ReadBranches(t.Context(), gitlabOrg, []string{"settings-cleanup", "qa-report-update", "blockers-page-single-template"})
	require.NoError(t, err)

	require.Len(t, fake.calls, 1)
	seen := fake.last(t)
	assert.Equal(t, readBranchesQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"path":     "acme/widgets",
		"branches": []any{"settings-cleanup", "qa-report-update", "blockers-page-single-template"},
		"first":    float64(maxPerPage),
		"after":    nil,
	}, seen.Variables)

	assert.Equal(t, forge.RateLimit{
		Known:     true,
		Remaining: 1999,
		Limit:     2000,
		ResetAt:   time.Unix(1789900000, 0),
	}, reading.RateLimit)

	require.Len(t, reading.PullRequests, 2, "the fork's merge request is left out")
	running, merged := reading.PullRequests[0], reading.PullRequests[1]
	assert.Equal(t, 256538, running.Number)
	assert.Equal(t, &forge.MergeReadiness{
		Decision:     forge.ReviewDecisionNone,
		Checks:       forge.ChecksPending,
		Mergeability: forge.MergeabilityBlocked,
		AutoMerge:    forge.AutoMerge{On: true, Allowed: true},
	}, running.MergeReadiness)
	assert.Equal(t, 256531, merged.Number)
	assert.Equal(t, &forge.MergeReadiness{
		Decision:     forge.ReviewDecisionApproved,
		Checks:       forge.ChecksPassing,
		Mergeability: forge.MergeabilityUnknown,
		AutoMerge:    forge.AutoMerge{},
	}, merged.MergeReadiness)
}

// TestReadBranchesPages covers following the cursor across pages, the newest five merge requests
// of a branch kept with a fork's counted among them, a branch left out of the query once it has
// five, and branches beyond GitLab's filter limit sent in further queries.
func TestReadBranchesPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		branches  []string
		pages     map[string]string
		wantSent  []int
		wantAfter []any
		wantRead  []int
	}{
		{
			name:      "no branches asks nothing",
			wantSent:  []int{},
			wantAfter: []any{},
			wantRead:  []int{},
		},
		{
			name:      "one branch with nothing open",
			branches:  []string{"quiet"},
			pages:     map[string]string{"": listing(false, "")},
			wantSent:  []int{1},
			wantAfter: []any{nil},
			wantRead:  []int{},
		},
		{
			name:     "a branch keeps its five newest across pages, a fork among them",
			branches: []string{"busy", "other"},
			pages: map[string]string{
				"": listing(true, "p1",
					mergeRequestFrom("busy", 10, true),
					mergeRequestFrom("busy", 9, false),
					mergeRequestFrom("other", 8, false),
					mergeRequestFrom("busy", 7, false),
				),
				"p1": listing(false, "p2",
					mergeRequestFrom("busy", 6, false),
					mergeRequestFrom("busy", 5, false),
					mergeRequestFrom("busy", 4, false),
					mergeRequestFrom("other", 3, false),
				),
			},
			wantSent:  []int{2, 2},
			wantAfter: []any{nil, "p1"},
			wantRead:  []int{9, 8, 7, 6, 5, 3},
		},
		{
			name:     "a branch with its five read leaves the query",
			branches: []string{"busy", "other"},
			pages: map[string]string{
				"": listing(true, "p1",
					mergeRequestFrom("busy", 6, false),
					mergeRequestFrom("busy", 5, false),
					mergeRequestFrom("busy", 4, false),
					mergeRequestFrom("busy", 3, false),
					mergeRequestFrom("busy", 2, false),
				),
				"p1": listing(false, "", mergeRequestFrom("other", 1, false)),
			},
			wantSent:  []int{2, 1},
			wantAfter: []any{nil, "p1"},
			wantRead:  []int{6, 5, 4, 3, 2, 1},
		},
		{
			name:      "more branches than gitlab filters by at once",
			branches:  xslice.Map(make([]string, maxSourceBranches+1), func(string) string { return "b" }),
			pages:     map[string]string{"": listing(false, "")},
			wantSent:  []int{maxSourceBranches, 1},
			wantAfter: []any{nil, nil},
			wantRead:  []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, nil)
			fake.handle = func(seen call) (answer, bool) {
				after, _ := seen.Variables["after"].(string)
				body, ok := tt.pages[after]

				return answer{body: body}, ok
			}

			reading, err := f.ReadBranches(t.Context(), gitlabOrg, tt.branches)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSent, xslice.Map(fake.calls, func(seen call) int {
				return len(seen.Variables["branches"].([]any))
			}))
			assert.Equal(t, tt.wantAfter, xslice.Map(fake.calls, func(seen call) any {
				return seen.Variables["after"]
			}))
			assert.Equal(t, tt.wantRead, xslice.Map(reading.PullRequests, func(pr forge.PullRequest) int {
				return pr.Number
			}))
		})
	}
}

// TestReadBranchesShrinksOnComplexity covers GitLab refusing a page for its complexity: the page
// is halved until it fits and stays that size, and a page of one refused is an error.
func TestReadBranchesShrinksOnComplexity(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)
	fake.handle = func(seen call) (answer, bool) {
		if seen.Variables["first"].(float64) > 25 {
			return answer{body: tooComplex}, true
		}
		if seen.Variables["after"] == nil {
			return answer{body: listing(true, "p1", mergeRequestFrom("one", 2, false))}, true
		}

		return answer{body: listing(false, "", mergeRequestFrom("two", 1, false))}, true
	}

	reading, err := f.ReadBranches(t.Context(), gitlabOrg, []string{"one", "two"})
	require.NoError(t, err)
	assert.Len(t, reading.PullRequests, 2)
	assert.Equal(t, []any{100.0, 50.0, 25.0, 25.0}, xslice.Map(fake.calls, func(seen call) any {
		return seen.Variables["first"]
	}))

	refused, _, _ := serve(t, map[string]answer{"ReadBranches": {body: tooComplex}})
	_, err = refused.ReadBranches(t.Context(), gitlabOrg, []string{"one"})
	require.ErrorIs(t, err, errTooComplex)
}
