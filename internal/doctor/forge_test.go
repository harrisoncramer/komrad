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
	}{
		{
			name:       "nothing connected",
			wantStatus: StatusOK,
			wantDetail: "none connected",
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
				{Host: "gitlab.com", Err: errors.New("Bad credentials")},
			},
			wantStatus: StatusWarn,
			wantDetail: "github.com as harrisoncramer",
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
				VerifyForges: func(context.Context, string) ([]ForgeStatus, error) {
					return tt.statuses, tt.err
				},
			})

			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
			if tt.wantCode != "" {
				assert.Equal(t, tt.wantCode, codeOf(result.Err))
			}
		})
	}
}
