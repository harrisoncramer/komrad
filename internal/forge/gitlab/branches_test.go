package gitlab

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

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
		Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE,
		Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED,
		AutoMerge:    forge.AutoMerge{On: true, Allowed: true},
		Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{{
			ProviderID: "2051234567",
			Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE,
			Name:       "pipeline",
			State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
			StartedAt:  xptr.Ptr(time.Date(2026, 9, 20, 17, 50, 0, 0, time.UTC)),
			Required:   true,
			URL:        "https://gitlab.com/acme/widgets/-/pipelines/2051234567",
		}}},
	}, running.MergeReadiness)
	assert.Equal(t, 256531, merged.Number)
	assert.Equal(t, &forge.MergeReadiness{
		Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
		Mergeability: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
		AutoMerge:    forge.AutoMerge{},
		Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{{
			ProviderID: "2051230000",
			Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE,
			Name:       "pipeline",
			State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
			Conclusion: "success",
			StartedAt:  xptr.Ptr(time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)),
			FinishedAt: xptr.Ptr(time.Date(2026, 9, 20, 10, 20, 0, 0, time.UTC)),
			Required:   true,
			URL:        "https://gitlab.com/acme/widgets/-/pipelines/2051230000",
		}}},
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

const tooComplex = `{
	"errors": [
		{
			"message": "Query has complexity of 294, which exceeds max complexity of 200"
		}
	]
}`

// readBranchesAnswer is GitLab answering a read of branches, recorded on 21 September against
// acme/widgets with made-up auto-merge fields: a running pipeline set to merge when it passes,
// a fork's merge request, and an approved merge request whose pipeline passed.
var readBranchesAnswer = listing(false, "end", openMergeRequestRunning, forkMergeRequest, mergedMergeRequestPassed)

const openMergeRequestRunning = `{
	"id": "gid://gitlab/MergeRequest/535736457",
	"iid": "256538",
	"title": "Strip undeclared keys from the settings",
	"webUrl": "https://gitlab.com/acme/widgets/-/merge_requests/256538",
	"state": "opened",
	"draft": false,
	"createdAt": "2026-09-20T08:58:11Z",
	"updatedAt": "2026-09-20T17:54:15Z",
	"mergedAt": null,
	"closedAt": null,
	"sourceBranch": "settings-cleanup",
	"targetBranch": "master",
	"diffHeadSha": "14a5732c18c7186fb0b34d469255b9609e4d11f2",
	"sourceProject": {
		"webUrl": "https://gitlab.com/acme/widgets"
	},
	"targetProject": {
		"webUrl": "https://gitlab.com/acme/widgets"
	},
	"diffRefs": {
		"baseSha": "4aabd419a0202362695a5e4147df061fbf976c9f"
	},
	"approvalsRequired": 0,
	"approvalsLeft": 0,
	"approvedBy": {
		"count": 0
	},
	"reviewers": {
		"nodes": [
			{
				"mergeRequestInteraction": {
					"reviewState": "REVIEW_STARTED"
				}
			}
		]
	},
	"headPipeline": {
		"id": "gid://gitlab/Ci::Pipeline/2051234567",
		"sha": "14a5732c18c7186fb0b34d469255b9609e4d11f2",
		"status": "RUNNING",
		"mergeRequestEventType": null,
		"path": "/acme/widgets/-/pipelines/2051234567",
		"startedAt": "2026-09-20T17:50:00Z",
		"finishedAt": null
	},
	"detailedMergeStatus": "NOT_APPROVED",
	"autoMergeEnabled": true,
	"availableAutoMergeStrategies": [
		"merge_when_checks_pass"
	]
}`

const mergedMergeRequestPassed = `{
	"id": "gid://gitlab/MergeRequest/535718519",
	"iid": "256531",
	"title": "Update the runtime data for the e2e tests",
	"webUrl": "https://gitlab.com/acme/widgets/-/merge_requests/256531",
	"state": "merged",
	"draft": false,
	"createdAt": "2026-09-20T05:24:39Z",
	"updatedAt": "2026-09-20T06:24:05Z",
	"mergedAt": "2026-09-20T06:24:05Z",
	"closedAt": null,
	"sourceBranch": "qa-report-update",
	"targetBranch": "master",
	"diffHeadSha": "f7ae81a86c306ee75b2703ea62137b787950f3f2",
	"sourceProject": {
		"webUrl": "https://gitlab.com/acme/widgets"
	},
	"targetProject": {
		"webUrl": "https://gitlab.com/acme/widgets"
	},
	"diffRefs": {
		"baseSha": "fb3b0f26554913f30d92e12c39b1814eda5b9a63"
	},
	"approvalsRequired": 2,
	"approvalsLeft": 0,
	"approvedBy": {
		"count": 3
	},
	"reviewers": {
		"nodes": [
			{
				"mergeRequestInteraction": {
					"reviewState": "APPROVED"
				}
			}
		]
	},
	"headPipeline": {
		"id": "gid://gitlab/Ci::Pipeline/2051230000",
		"sha": "f7ae81a86c306ee75b2703ea62137b787950f3f2",
		"status": "SUCCESS",
		"mergeRequestEventType": null,
		"path": "/acme/widgets/-/pipelines/2051230000",
		"startedAt": "2026-09-20T10:00:00Z",
		"finishedAt": "2026-09-20T10:20:00Z"
	},
	"detailedMergeStatus": "MERGEABLE",
	"autoMergeEnabled": false,
	"availableAutoMergeStrategies": []
}`

// mergeRequestFrom is the running merge request renumbered and moved to a branch, in a fork when
// fork is set.
func mergeRequestFrom(branch string, iid int, fork bool) string {
	node := strings.ReplaceAll(openMergeRequestRunning, "256538", strconv.Itoa(iid))
	node = strings.Replace(node, `"sourceBranch": "settings-cleanup"`, `"sourceBranch": "`+branch+`"`, 1)
	if fork {
		node = strings.Replace(node, `"webUrl": "https://gitlab.com/acme/widgets"`, `"webUrl": "https://gitlab.com/acme-forks/widgets"`, 1)
	}

	return node
}
