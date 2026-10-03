package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestForgesCheck covers no forges, every forge answering, and ones in trouble, which warn with
// the fix their kind of forge takes and still name the ones that answered.
func TestForgesCheck(t *testing.T) {
	tests := []struct {
		name       string
		statuses   []ForgeStatus
		err        error
		wantStatus Status
		wantDetail string
		wantFix    string
		wantCode   string
		wantErr    string
	}{
		{
			name:       "nothing connected",
			wantStatus: StatusWarn,
			wantFix:    connectFirstFix,
			wantCode:   "no_forges",
		},
		{
			name: "every forge answers",
			statuses: []ForgeStatus{
				{Host: "github.com", Login: "harrisoncramer"},
				{Host: "gitlab.com", Login: "harrisoncramer"},
			},
			wantStatus: StatusOK,
			wantDetail: "github.com as harrisoncramer, gitlab.com as harrisoncramer",
		},
		{
			name: "one forge refuses",
			statuses: []ForgeStatus{
				{Host: "github.com", Login: "harrisoncramer"},
				{
					Host:        "gitlab.com",
					Kind:        "gitlab",
					Method:      "token",
					Problem:     "Bad credentials",
					ProblemKind: "refused",
				},
			},
			wantStatus: StatusWarn,
			wantDetail: "github.com as harrisoncramer",
			wantFix:    "komrad forge connect gitlab.com --token-file <a personal access token with the api scope>",
			wantCode:   "forge_refused",
		},
		{
			name: "one forge cannot be reached",
			statuses: []ForgeStatus{
				{
					Host:        "ghe.example.com",
					Kind:        "github",
					Method:      "token",
					Problem:     "dial tcp: no such host",
					ProblemKind: "unreachable",
				},
			},
			wantStatus: StatusWarn,
			wantCode:   "forge_unreachable",
		},
		{
			name: "every failing host is named and the refusal sets the code",
			statuses: []ForgeStatus{
				{
					Host:        "github.com",
					Kind:        "github",
					Method:      "token",
					Problem:     "no credential file for github.com; connect it again",
					ProblemKind: "no_credential",
				},
				{
					Host:        "gitlab.com",
					Kind:        "gitlab",
					Method:      "token",
					Problem:     "Bad credentials",
					ProblemKind: "refused",
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "gh auth token --hostname github.com | komrad forge connect github.com; komrad forge connect gitlab.com --token-file <a personal access token with the api scope>",
			wantCode:   "forge_refused",
			wantErr:    "github.com: no credential file for github.com; connect it again; gitlab.com: Bad credentials",
		},
		{
			name: "one forge has no credential file",
			statuses: []ForgeStatus{
				{
					Host:        "github.com",
					Kind:        "github",
					Method:      "token",
					Problem:     "no credential file for github.com; connect it again",
					ProblemKind: "no_credential",
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "gh auth token --hostname github.com | komrad forge connect github.com",
			wantCode:   "forge_no_credential",
		},
		{
			name: "a self-hosted gitlab names its kind",
			statuses: []ForgeStatus{
				{
					Host:        "gitlab.example.com",
					Kind:        "gitlab",
					Method:      "token",
					Problem:     "401 Unauthorized",
					ProblemKind: "refused",
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "komrad forge connect gitlab.example.com --kind gitlab --token-file <a personal access token with the api scope>",
			wantCode:   "forge_refused",
		},
		{
			name: "a github app is connected as the app again",
			statuses: []ForgeStatus{
				{
					Host:        "github.com",
					Kind:        "github",
					Method:      "github_app",
					Problem:     "installation removed",
					ProblemKind: "refused",
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "komrad forge connect github.com --method github-app",
			wantCode:   "forge_refused",
		},
		{
			name:       "the connections cannot be read",
			err:        errors.New("no such table"),
			wantStatus: StatusWarn,
			wantCode:   "forges_unreadable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newForgesCheck().Run(t.Context(), Env{
				CheckForges: func(context.Context) ([]ForgeStatus, error) {
					return tt.statuses, tt.err
				},
			})

			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
			assert.Equal(t, tt.wantFix, result.Fix)
			if tt.wantCode != "" {
				assert.Equal(t, tt.wantCode, codeOf(result.Err))
			}
			if tt.wantErr != "" {
				assert.ErrorContains(t, result.Err, tt.wantErr)
			}
		})
	}
}
