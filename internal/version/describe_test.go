package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDescribe covers naming a build where the version alone cannot, since every build from a
// worktree reports devel.
func TestDescribe(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{
			name:    "a build naming no commit",
			version: "v0.0.0-20260916203018-6da018be0403",
			want:    "v0.0.0-20260916203018-6da018be0403",
		},
		{
			name:    "a worktree build, told apart by its commit alone",
			version: "devel",
			commit:  "6da018be0403648b090dfa7695d160efea180b49",
			want:    "devel (6da018b)",
		},
		{
			name:    "a commit shorter than the short form",
			version: "devel",
			commit:  "6da018",
			want:    "devel (6da018)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Describe(tt.version, tt.commit))
		})
	}
}
