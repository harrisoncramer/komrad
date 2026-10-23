package doctor

import (
	"context"
	"errors"
	"testing"

	forgev1 "komrad/proto/gen/forge/v1"

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
					Kind:        forgev1.Kind_KIND_GITLAB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
					Problem:     "Bad credentials",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_REFUSED,
				},
			},
			wantStatus: StatusWarn,
			wantDetail: "github.com as harrisoncramer",
			wantFix:    "komrad forge connect gitlab.com --token-file <token>",
			wantCode:   "forge_refused",
		},
		{
			name: "one forge cannot be reached",
			statuses: []ForgeStatus{
				{
					Host:        "ghe.example.com",
					Kind:        forgev1.Kind_KIND_GITHUB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
					Problem:     "dial tcp: no such host",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_UNREACHABLE,
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
					Kind:        forgev1.Kind_KIND_GITHUB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
					Problem:     "no credential file for github.com; connect it again",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_NO_CREDENTIAL,
				},
				{
					Host:        "gitlab.com",
					Kind:        forgev1.Kind_KIND_GITLAB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
					Problem:     "Bad credentials",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_REFUSED,
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "komrad forge connect github.com --token-file <token>; komrad forge connect gitlab.com --token-file <token>",
			wantCode:   "forge_refused",
			wantErr:    "github.com: no credential file for github.com; connect it again; gitlab.com: Bad credentials",
		},
		{
			name: "one forge has no credential file",
			statuses: []ForgeStatus{
				{
					Host:        "github.com",
					Kind:        forgev1.Kind_KIND_GITHUB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
					Problem:     "no credential file for github.com; connect it again",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_NO_CREDENTIAL,
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "komrad forge connect github.com --token-file <token>",
			wantCode:   "forge_no_credential",
		},
		{
			name: "a self-hosted gitlab names its kind",
			statuses: []ForgeStatus{
				{
					Host:        "gitlab.example.com",
					Kind:        forgev1.Kind_KIND_GITLAB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
					Problem:     "401 Unauthorized",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_REFUSED,
				},
			},
			wantStatus: StatusWarn,
			wantFix:    "komrad forge connect gitlab.example.com --kind gitlab --token-file <token>",
			wantCode:   "forge_refused",
		},
		{
			name: "a github app is connected as the app again",
			statuses: []ForgeStatus{
				{
					Host:        "github.com",
					Kind:        forgev1.Kind_KIND_GITHUB,
					Method:      forgev1.CredentialMethod_CREDENTIAL_METHOD_GITHUB_APP,
					Problem:     "installation removed",
					ProblemKind: forgev1.ProblemKind_PROBLEM_KIND_REFUSED,
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
