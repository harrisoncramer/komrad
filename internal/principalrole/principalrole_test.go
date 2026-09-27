package principalrole_test

import (
	"testing"

	"komrad/internal/principalrole"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParse covers which spellings are roles.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spelled string
		want    principalrole.Role
		wantErr string
	}{
		{
			name:    "an empty role is refused",
			spelled: "",
			wantErr: `unknown role "", want one of viewer, operator, automation`,
		},
		{
			name:    "a role spelled in capitals is refused",
			spelled: "Viewer",
			wantErr: `unknown role "Viewer"`,
		},
		{
			name:    "a role komrad does not have is refused",
			spelled: "admin",
			wantErr: `unknown role "admin"`,
		},
		{
			name:    "viewer is read",
			spelled: "viewer",
			want:    principalrole.Viewer,
		},
		{
			name:    "operator is read",
			spelled: "operator",
			want:    principalrole.Operator,
		},
		{
			name:    "automation is read",
			spelled: "automation",
			want:    principalrole.Automation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := principalrole.Parse(tt.spelled)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, principalrole.ErrUnknown)
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
