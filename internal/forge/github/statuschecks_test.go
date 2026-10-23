package github

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadBranchesStatusCheckListing covers what an open pull request's listing holds when GitHub's
// answer for its head is missing, for another commit, or blank.
func TestReadBranchesStatusCheckListing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		checks string
		steps  string
		want   *forge.StatusCheckListing
	}{
		{
			name: "a pull request github no longer finds lists nothing",
			checks: data(`{
	"p0": null
}`),
		},
		{
			name: "an answer for a commit pushed since the read lists nothing",
			checks: data(`{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
						"statusCheckRollup": {
							"contexts": {
								"nodes": [
									{
										"__typename": "CheckRun",
										"id": "CR_1",
										"name": "test",
										"status": "IN_PROGRESS"
									}
								]
							}
						}
					}
				}
			]
		}
	}
}`),
		},
		{
			name: "a head with no checks yet is a blank listing",
			checks: data(`{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
						"statusCheckRollup": null
					}
				}
			]
		}
	}
}`),
			want: &forge.StatusCheckListing{},
		},
		{
			name: "checks in flight are queued or running and checks that cannot start alone are waiting",
			checks: data(`{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
						"statusCheckRollup": {
							"contexts": {
								"nodes": [
									{
										"__typename": "CheckRun",
										"id": "CR_1",
										"name": "test",
										"status": "IN_PROGRESS",
										"startedAt": "2026-09-20T11:00:00Z"
									},
									{
										"__typename": "StatusContext",
										"id": "SC_1",
										"context": "ci/legacy",
										"state": "PENDING",
										"createdAt": "2026-09-20T11:01:00Z"
									},
									{
										"__typename": "StatusContext",
										"id": "SC_2",
										"context": "ci/deploy",
										"state": "EXPECTED"
									},
									{
										"__typename": "CheckRun",
										"id": "CR_2",
										"name": "deploy",
										"status": "WAITING"
									},
									{
										"__typename": "CheckRun",
										"id": "CR_3",
										"name": "lint",
										"status": "PENDING"
									}
								]
							}
						}
					}
				}
			]
		}
	}
}`),
			want: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
				{
					ProviderID:     "CR_1",
					Kind:           pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:           "test",
					State:          pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
					StartedAt:      xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
					StepsTruncated: true,
				},
				{
					ProviderID: "SC_1",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_STATUS,
					Name:       "ci/legacy",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
					StartedAt:  xptr.Ptr(time.Date(2026, 9, 20, 11, 1, 0, 0, time.UTC)),
				},
				{
					ProviderID: "SC_2",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_STATUS,
					Name:       "ci/deploy",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_WAITING,
				},
				{
					ProviderID:     "CR_2",
					Kind:           pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:           "deploy",
					State:          pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_WAITING,
					StepsTruncated: true,
				},
				{
					ProviderID:     "CR_3",
					Kind:           pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:           "lint",
					State:          pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED,
					StepsTruncated: true,
				},
			}},
		},
		{
			name: "a head with more checks than one page is a truncated listing",
			checks: data(`{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
						"statusCheckRollup": {
							"contexts": {
								"pageInfo": {
									"hasNextPage": true
								},
								"nodes": [
									{
										"__typename": "CheckRun",
										"id": "CR_1",
										"name": "test",
										"status": "QUEUED"
									}
								]
							}
						}
					}
				}
			]
		}
	}
}`),
			want: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{{
				ProviderID:     "CR_1",
				Kind:           pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
				Name:           "test",
				State:          pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED,
				StepsTruncated: true,
			}}, Truncated: true},
		},
		{
			name: "a check run lists its steps and says when it has more than one page of them",
			checks: data(`{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
						"statusCheckRollup": {
							"contexts": {
								"nodes": [
									{
										"__typename": "CheckRun",
										"id": "CR_1",
										"name": "test",
										"status": "IN_PROGRESS"
									},
									{
										"__typename": "CheckRun",
										"id": "CR_2",
										"name": "lint",
										"status": "QUEUED"
									},
									{
										"__typename": "CheckRun",
										"id": "CR_3",
										"name": "build",
										"status": "QUEUED"
									}
								]
							}
						}
					}
				}
			]
		}
	}
}`),
			steps: data(`{
	"s0": [
		{
			"id": "CR_1",
			"steps": {
				"pageInfo": {
					"hasNextPage": true
				},
				"nodes": [
					{
						"number": 1,
						"name": "Set up job",
						"status": "COMPLETED",
						"conclusion": "SUCCESS",
						"startedAt": "2026-09-20T11:00:00Z",
						"completedAt": "2026-09-20T11:00:05Z"
					},
					{
						"number": 2,
						"name": "Run tests",
						"status": "IN_PROGRESS",
						"startedAt": "2026-09-20T11:00:05Z"
					}
				]
			}
		},
		{
			"id": "CR_2",
			"steps": {
				"pageInfo": {
					"hasNextPage": false
				},
				"nodes": []
			}
		},
		null
	]
}`),
			want: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
				{
					ProviderID: "CR_1",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "test",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
					Steps: []forge.StatusCheckStep{
						{
							Number:     1,
							Name:       "Set up job",
							State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
							Conclusion: "success",
							StartedAt:  xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
							FinishedAt: xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 5, 0, time.UTC)),
						},
						{
							Number:    2,
							Name:      "Run tests",
							State:     pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
							StartedAt: xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 5, 0, time.UTC)),
						},
					},
					StepsTruncated: true,
				},
				{
					ProviderID: "CR_2",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "lint",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED,
				},
				{
					ProviderID:     "CR_3",
					Kind:           pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:           "build",
					State:          pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED,
					StepsTruncated: true,
				},
			}},
		},
		{
			name: "a steps read github refuses keeps the checks and the steps stored for each run",
			checks: data(`{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
						"statusCheckRollup": {
							"contexts": {
								"nodes": [
									{
										"__typename": "CheckRun",
										"id": "CR_1",
										"name": "test",
										"status": "QUEUED"
									}
								]
							}
						}
					}
				}
			]
		}
	}
}`),
			steps: `{
	"data": null,
	"errors": [
		{
			"message": "Something went wrong while executing your query."
		}
	]
}`,
			want: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{{
				ProviderID:     "CR_1",
				Kind:           pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
				Name:           "test",
				State:          pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED,
				StepsTruncated: true,
			}}},
		},
		{
			name: "a check read github refuses lists nothing and keeps the branch read",
			checks: `{
	"data": null,
	"errors": [
		{
			"message": "Something went wrong while executing your query."
		}
	]
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			answers := map[string]answer{
				"ReadBranches":     {body: branchesRead(withMergeReadiness(openPull, failingMergeReadiness))},
				"ReadStatusChecks": {body: tt.checks},
			}
			if tt.steps != "" {
				answers["ReadCheckSteps"] = answer{body: tt.steps}
			}
			f, _, _ := serve(t, answers)

			reading, err := f.ReadBranches(t.Context(), fixture, []string{"fixture/open"})
			require.NoError(t, err)
			require.Len(t, reading.PullRequests, 1)
			assert.Equal(t, tt.want, reading.PullRequests[0].MergeReadiness.Listing)
		})
	}
}
