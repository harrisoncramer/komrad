package gitlab

import (
	"encoding/json"
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestRereview covers asking each reviewer who requested changes to review again, and
// refusing a finished merge request or one nobody asked changes of.
func TestRequestRereview(t *testing.T) {
	t.Parallel()

	const address = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/7"

	target := func(state, reviewers string) answer {
		return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"state": "` + state + `",
				"reviewers": {
					"nodes": [
` + reviewers + `
					]
				}
			}
		}
	}
}`}
	}
	const reviewers = `{
	"id": "gid://gitlab/User/1",
	"mergeRequestInteraction": {
		"reviewState": "REQUESTED_CHANGES"
	}
},
{
	"id": "gid://gitlab/User/2",
	"mergeRequestInteraction": {
		"reviewState": "REVIEWED"
	}
},
{
	"id": "gid://gitlab/User/3",
	"mergeRequestInteraction": {
		"reviewState": "REQUESTED_CHANGES"
	}
}`
	rereviewed := answer{body: `{
	"data": {
		"mergeRequestReviewerRereview": {
			"errors": []
		}
	}
}`}

	tests := []struct {
		name      string
		answers   map[string]answer
		wantErr   error
		wantUsers []any
	}{
		{
			name:      "every reviewer asking for changes is asked again",
			answers:   map[string]answer{"RereviewTarget": target("opened", reviewers), "Rereview": rereviewed},
			wantUsers: []any{"gid://gitlab/User/1", "gid://gitlab/User/3"},
		},
		{
			name:    "a merged merge request is refused",
			answers: map[string]answer{"RereviewTarget": target("merged", reviewers)},
			wantErr: forge.ErrNoRereview,
		},
		{
			name:    "a closed merge request is refused",
			answers: map[string]answer{"RereviewTarget": target("closed", reviewers)},
			wantErr: forge.ErrNoRereview,
		},
		{
			name: "a merge request nobody asked changes of is refused",
			answers: map[string]answer{"RereviewTarget": target("opened", `{
	"id": "gid://gitlab/User/2",
	"mergeRequestInteraction": {
		"reviewState": "REVIEWED"
	}
}`)},
			wantErr: forge.ErrNoRereview,
		},
		{
			name: "a review request gitlab will not make",
			answers: map[string]answer{
				"RereviewTarget": target("opened", reviewers),
				"Rereview": {body: `{
	"data": {
		"mergeRequestReviewerRereview": {
			"errors": [
				"You don't have permission to request a review"
			]
		}
	}
}`},
			},
			wantErr:   forge.ErrNoRereview,
			wantUsers: []any{"gid://gitlab/User/1"},
		},
		{
			name:    "a merge request gitlab does not have",
			answers: map[string]answer{"RereviewTarget": {body: missingMergeRequest}},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.RequestRereview(t.Context(), address)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			var users []any
			for _, sent := range xslice.Filter(fake.calls, func(c call) bool { return c.Operation == "Rereview" }) {
				assert.Equal(t, "harrisoncramer/komrad", sent.Variables["path"])
				assert.Equal(t, "7", sent.Variables["iid"])
				users = append(users, sent.Variables["user"])
			}
			assert.Equal(t, tt.wantUsers, users)
		})
	}
}

// TestDecisionFolds covers how the approvals become komrad's word: a reviewer asking for
// changes first, then approvals still owed, then anybody having approved, else nothing.
func TestDecisionFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node string
		want pullrequestv1.ReviewDecision
	}{
		{
			name: "a reviewer asking for changes beats an approval",
			node: `{
	"approvalsRequired": 1,
	"approvalsLeft": 0,
	"approvedBy": {
		"count": 1
	},
	"reviewers": {
		"nodes": [
			{
				"mergeRequestInteraction": {
					"reviewState": "REQUESTED_CHANGES"
				}
			},
			{
				"mergeRequestInteraction": {
					"reviewState": "APPROVED"
				}
			}
		]
	}
}`,
			want: pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED,
		},
		{
			name: "approvals still owed are pending whoever has approved",
			node: `{
	"approvalsRequired": 2,
	"approvalsLeft": 2,
	"approvedBy": {
		"count": 1
	},
	"reviewers": {
		"nodes": [
			{
				"mergeRequestInteraction": {
					"reviewState": "UNAPPROVED"
				}
			},
			{
				"mergeRequestInteraction": {
					"reviewState": "APPROVED"
				}
			},
			{
				"mergeRequestInteraction": {
					"reviewState": "REVIEWED"
				}
			}
		]
	}
}`,
			want: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
		},
		{
			name: "approved",
			node: `{
	"approvalsRequired": 1,
	"approvalsLeft": 0,
	"approvedBy": {
		"count": 1
	},
	"reviewers": {
		"nodes": [
			{
				"mergeRequestInteraction": {
					"reviewState": "APPROVED"
				}
			}
		]
	}
}`,
			want: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
		},
		{
			name: "no rule and nobody has approved is nothing, whatever the reviewers did",
			node: `{
	"approvalsRequired": 0,
	"approvalsLeft": 0,
	"approvedBy": {
		"count": 0
	},
	"reviewers": {
		"nodes": [
			{
				"mergeRequestInteraction": {
					"reviewState": "REVIEWED"
				}
			}
		]
	}
}`,
			want: pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var node mergeReadinessNode
			require.NoError(t, json.Unmarshal([]byte(tt.node), &node))
			assert.Equal(t, tt.want, node.decision())
		})
	}
}
