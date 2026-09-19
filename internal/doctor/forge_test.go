package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestForgesCheck covers no forges, every forge answering, and one that refuses, which warns
// and still names the ones that answered.
func TestForgesCheck(t *testing.T) {
	tests := []struct {
		name       string
		statuses   []ForgeStatus
		err        error
		wantStatus Status
		wantDetail string
		wantCode   string
		wantErr    string
	}{
		{
			name:       "nothing connected",
			wantStatus: StatusWarn,
			wantDetail: "gh auth token | weave forge connect github.com",
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
					Problem:     "Bad credentials",
					ProblemKind: "refused",
				},
			},
			wantStatus: StatusWarn,
			wantDetail: "github.com as harrisoncramer",
			wantCode:   "forge_refused",
		},
		{
			name: "one forge cannot be reached",
			statuses: []ForgeStatus{
				{
					Host:        "ghe.example.com",
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
					Problem:     "no credential file for github.com; connect it again",
					ProblemKind: "no_credential",
				},
				{
					Host:        "gitlab.com",
					Problem:     "Bad credentials",
					ProblemKind: "refused",
				},
			},
			wantStatus: StatusWarn,
			wantCode:   "forge_refused",
			wantErr:    "github.com: no credential file for github.com; connect it again; gitlab.com: Bad credentials",
		},
		{
			name: "one forge has no credential file",
			statuses: []ForgeStatus{
				{
					Host:        "github.com",
					Problem:     "no credential file for github.com; connect it again",
					ProblemKind: "no_credential",
				},
			},
			wantStatus: StatusWarn,
			wantCode:   "forge_no_credential",
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
			if tt.wantCode != "" {
				assert.Equal(t, tt.wantCode, codeOf(result.Err))
			}
			if tt.wantErr != "" {
				assert.Contains(t, result.Err.Error(), tt.wantErr)
			}
		})
	}
}
