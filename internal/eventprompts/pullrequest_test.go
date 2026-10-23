package eventprompts

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/stretchr/testify/assert"
)

// TestTheMailNamesEachFailingCheck pins the noun and number of the forge the pull request is on,
// and the failing checks, each with its link, split by whether they are news to the agent.
func TestTheMailNamesEachFailingCheck(t *testing.T) {
	test := forge.StatusCheck{Name: "test", URL: "https://github.com/harrisoncramer/komrad/runs/1"}
	deploy := forge.StatusCheck{Name: "deploy"}
	heldSince := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	before := heldSince.Add(-time.Minute)

	tests := []struct {
		name         string
		forge        forgev1.Kind
		failures     []pullrequeststore.CheckFailure
		heldSince    *time.Time
		wantLines    []string
		wantNotLines []string
	}{
		{
			name:         "github",
			forge:        forgev1.Kind_KIND_GITHUB,
			wantLines:    []string{"komrad: pull request #7 ("},
			wantNotLines: []string{"New failures:", "Still failing:"},
		},
		{
			name:      "gitlab",
			forge:     forgev1.Kind_KIND_GITLAB,
			wantLines: []string{"komrad: merge request !7 ("},
		},
		{
			name:         "a failure the agent was not told of is new with its link",
			forge:        forgev1.Kind_KIND_GITHUB,
			failures:     []pullrequeststore.CheckFailure{{Check: test}},
			wantLines:    []string{"New failures:\n  test: https://github.com/harrisoncramer/komrad/runs/1\nReview comments"},
			wantNotLines: []string{"Still failing:"},
		},
		{
			name:      "a failing check without a link says so",
			forge:     forgev1.Kind_KIND_GITHUB,
			failures:  []pullrequeststore.CheckFailure{{Check: deploy}},
			wantLines: []string{"New failures:\n  deploy: no link\n"},
		},
		{
			name:      "a failure the agent was told of before the held mail is still failing",
			forge:     forgev1.Kind_KIND_GITHUB,
			failures:  []pullrequeststore.CheckFailure{{Check: test, NotedAt: &before}, {Check: deploy}},
			heldSince: &heldSince,
			wantLines: []string{"New failures:\n  deploy: no link\nStill failing:\n  test: https://github.com/harrisoncramer/komrad/runs/1\nReview comments"},
		},
		{
			name:         "a failure the held mail told of is new",
			forge:        forgev1.Kind_KIND_GITHUB,
			failures:     []pullrequeststore.CheckFailure{{Check: test, NotedAt: &heldSince}},
			heldSince:    &heldSince,
			wantLines:    []string{"New failures:\n  test: "},
			wantNotLines: []string{"Still failing:"},
		},
		{
			name:         "a failure the agent was told of with no mail held is still failing",
			forge:        forgev1.Kind_KIND_GITHUB,
			failures:     []pullrequeststore.CheckFailure{{Check: test, NotedAt: &before}},
			wantLines:    []string{"Still failing:\n  test: "},
			wantNotLines: []string{"New failures:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := PullRequest{
				PullRequest: pullrequeststore.PullRequest{
					Forge:      tt.forge,
					Number:     7,
					HeadBranch: "agent/x",
				},
				Failures:  tt.failures,
				HeldSince: tt.heldSince,
			}.Body()

			for _, line := range tt.wantLines {
				assert.Contains(t, body, line)
			}
			for _, line := range tt.wantNotLines {
				assert.NotContains(t, body, line)
			}
		})
	}
}
