package cli

import (
	"testing"

	"weave/internal/server"

	"github.com/stretchr/testify/assert"
)

// TestSameBuild covers the two stampings of one commit, since the engine's binary is stamped by
// the deploy and the panel's by the toolchain.
func TestSameBuild(t *testing.T) {
	const commit = "6da018be0403648b090dfa7695d160efea180b49"

	const behind = "4b1db3cfa7f3eb6f9f5f4de5b4ba5f5e12f5a0c1"

	tests := []struct {
		name        string
		body        server.Health
		selfVersion string
		selfCommit  string
		want        bool
	}{
		{
			name:        "an engine and a panel built two ways from one commit",
			body:        server.Health{Version: "v0.0.0-engine.6da018be", Commit: commit},
			selfVersion: "v0.0.0-20260916203018-6da018be0403",
			selfCommit:  commit,
			want:        true,
		},
		{
			name:        "an engine left on an older commit",
			body:        server.Health{Version: "v0.0.0-engine.4b1db3cf", Commit: behind},
			selfVersion: "v0.0.0-20260916203018-6da018be0403",
			selfCommit:  commit,
			want:        false,
		},
		{
			name:        "a local server this binary started, neither of them naming a commit",
			body:        server.Health{Version: "devel"},
			selfVersion: "devel",
			selfCommit:  "",
			want:        true,
		},
		{
			name:        "a local server left running across a rebuild",
			body:        server.Health{Version: "v0.0.0-20260915101500-4b1db3cfa7f3"},
			selfVersion: "v0.0.0-20260916203018-6da018be0403",
			selfCommit:  commit,
			want:        false,
		},
		{
			name:        "a server naming a commit this build cannot match",
			body:        server.Health{Version: "v0.0.0-engine.6da018be", Commit: commit},
			selfVersion: "devel",
			selfCommit:  "",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sameBuild(tt.body, tt.selfVersion, tt.selfCommit))
		})
	}
}
