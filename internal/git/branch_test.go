//go:build integration

package git

import (
	"testing"

	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetCurrentBranch covers what the panel reads off an agent's directory, where anything git
// will not name a branch for has to come back as no branch rather than as a name.
func TestGetCurrentBranch(t *testing.T) {
	t.Parallel()

	onMain := repo(t)

	onFeature := repo(t)
	run(t, onFeature, "checkout", "--quiet", "-b", "feature/x")

	detached := repo(t)
	head, err := Run(t.Context(), detached, "rev-parse", "HEAD")
	require.NoError(t, err)
	run(t, detached, "checkout", "--quiet", head)

	tests := []struct {
		name string
		dir  string
		want *string
	}{
		{
			name: "a directory that is not a checkout names nothing",
			dir:  t.TempDir(),
			want: nil,
		},
		{
			name: "a detached head is not a named branch",
			dir:  detached,
			want: nil,
		},
		{
			name: "a checkout on main reads main",
			dir:  onMain,
			want: xptr.Ptr("main"),
		},
		{
			name: "a branch holding a slash keeps it whole",
			dir:  onFeature,
			want: xptr.Ptr("feature/x"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := client.GetCurrentBranch(t.Context(), tt.dir)

			if tt.want == nil {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, *tt.want, *got)
		})
	}
}
