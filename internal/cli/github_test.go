package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireGitHubRemote covers which checkouts the token is answered for, an agent working on
// another forge getting a refusal naming it rather than a gh that fails later.
func TestRequireGitHubRemote(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		wantErr string
	}{
		{
			name:   "an https remote on github",
			remote: "https://github.com/harrisoncramer/komrade.git",
		},
		{
			name:   "an ssh remote on github",
			remote: "git@github.com:harrisoncramer/komrade.git",
		},
		{
			name:   "a remote git cannot be asked about",
			remote: "",
		},
		{
			name:    "another forge",
			remote:  "git@gitlab.com:group/thing.git",
			wantErr: "group/thing is on gitlab.com",
		},
		{
			name:    "a github enterprise host the app was not made for",
			remote:  "https://ghe.example.com/team/thing.git",
			wantErr: "team/thing is on ghe.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireGitHubRemote(tt.remote)

			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, errNotGitHubCheckout)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
