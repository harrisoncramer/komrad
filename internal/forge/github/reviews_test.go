package github

import (
	"encoding/json"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestRereview covers asking each user who requested changes to review again, leaving out
// the bots and approvals, and refusing a finished pull request or one nobody asked changes of.
func TestRequestRereview(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	target := func(state, opinions string) answer {
		return answer{body: data(`{"repository":{"pullRequest":{"id":"PR_742","state":"` + state + `","latestOpinionatedReviews":{"nodes":[` + opinions + `]}}}}`)}
	}
	const opinions = `{"state":"CHANGES_REQUESTED","author":{"id":"U_ada"}},{"state":"APPROVED","author":{"id":"U_bob"}},{"state":"CHANGES_REQUESTED","author":{}},{"state":"CHANGES_REQUESTED","author":{"id":"U_cy"}}`
	requested := answer{body: data(`{"requestReviews":{"clientMutationId":null}}`)}

	tests := []struct {
		name      string
		answers   map[string]answer
		wantErr   error
		wantUsers []any
	}{
		{
			name:      "every user asking for changes is asked again",
			answers:   map[string]answer{"RereviewTarget": target("OPEN", opinions), "RequestReviews": requested},
			wantUsers: []any{"U_ada", "U_cy"},
		},
		{
			name:    "a merged pull request is refused before github is asked to change it",
			answers: map[string]answer{"RereviewTarget": target("MERGED", opinions)},
			wantErr: forge.ErrNoRereview,
		},
		{
			name:    "a pull request nobody asked changes of is refused",
			answers: map[string]answer{"RereviewTarget": target("OPEN", `{"state":"APPROVED","author":{"id":"U_bob"}}`)},
			wantErr: forge.ErrNoRereview,
		},
		{
			name: "a review request github will not make",
			answers: map[string]answer{
				"RereviewTarget": target("OPEN", opinions),
				"RequestReviews": {body: `{"data":{"requestReviews":null},"errors":[{"type":"UNPROCESSABLE","path":["requestReviews"],"message":"Could not resolve to a node"}]}`},
			},
			wantErr: forge.ErrNoRereview,
		},
		{
			name:    "a pull request github does not have",
			answers: map[string]answer{"RereviewTarget": {body: data(`{"repository":{"pullRequest":null}}`)}},
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

				return
			}
			require.NoError(t, err)
			seen := fake.last(t)
			assert.Equal(t, "RequestReviews", seen.Operation)
			assert.Equal(t, map[string]any{"id": "PR_742", "users": tt.wantUsers}, seen.Variables)
		})
	}
}

// TestDecision covers a change request read as a re-review requested only once every user who
// asked for changes has been asked to review again.
func TestDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		readiness string
		want      forge.ReviewDecision
	}{
		{
			name:      "every user asking for changes asked again",
			readiness: `{"reviewDecision":"CHANGES_REQUESTED","latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{"login":"ada"}},{"state":"APPROVED","author":{"login":"bob"}}]},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"cy"}},{"requestedReviewer":{"login":"ada"}}]}}`,
			want:      forge.ReviewDecisionRereviewRequested,
		},
		{
			name:      "one user asking for changes not asked again",
			readiness: `{"reviewDecision":"CHANGES_REQUESTED","latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{"login":"ada"}},{"state":"CHANGES_REQUESTED","author":{"login":"bob"}}]},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"ada"}}]}}`,
			want:      forge.ReviewDecisionChangesRequested,
		},
		{
			name:      "a team asked to review is not the user who asked for changes",
			readiness: `{"reviewDecision":"CHANGES_REQUESTED","latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{"login":"ada"}}]},"reviewRequests":{"nodes":[{"requestedReviewer":{}}]}}`,
			want:      forge.ReviewDecisionChangesRequested,
		},
		{
			name:      "a bot asking for changes is left out, as it cannot be asked again",
			readiness: `{"reviewDecision":"CHANGES_REQUESTED","latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{"login":"ada"}},{"state":"CHANGES_REQUESTED","author":{}}]},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"ada"}}]}}`,
			want:      forge.ReviewDecisionRereviewRequested,
		},
		{
			name:      "only a bot asking for changes",
			readiness: `{"reviewDecision":"CHANGES_REQUESTED","latestOpinionatedReviews":{"nodes":[{"state":"CHANGES_REQUESTED","author":{}}]},"reviewRequests":{"nodes":[]}}`,
			want:      forge.ReviewDecisionChangesRequested,
		},
		{
			name:      "changes requested with no verdict naming who asked",
			readiness: `{"reviewDecision":"CHANGES_REQUESTED","latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"ada"}}]}}`,
			want:      forge.ReviewDecisionChangesRequested,
		},
		{
			name:      "an approval asked to review again stays approved",
			readiness: `{"reviewDecision":"APPROVED","latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"ada"}}]},"reviewRequests":{"nodes":[{"requestedReviewer":{"login":"ada"}}]}}`,
			want:      forge.ReviewDecisionApproved,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var node mergeReadinessNode
			require.NoError(t, json.Unmarshal([]byte(tt.readiness), &node))
			assert.Equal(t, tt.want, node.decision())
		})
	}
}
