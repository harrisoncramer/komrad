package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGet covers what each way of building this binary reports; the panel reuses a server
// only when its version matches, so what matters is that every build has one and that two
// different builds never share it.
func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		want    string
	}{
		{
			name:    "no build information at all",
			stamped: "",
			info:    nil,
			want:    devel,
		},
		{
			name:    "a module the toolchain recorded no version for",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: ""}},
			want:    devel,
		},
		{
			name:    "a build with vcs stamping turned off",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want:    devel,
		},
		{
			name:    "an ordinary go build in a checkout",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba"}},
			want:    "v0.0.0-20260801021359-3b6495f546ba",
		},
		{
			name:    "a build from a dirty tree",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba+dirty"}},
			want:    "v0.0.0-20260801021359-3b6495f546ba+dirty",
		},
		{
			name:    "a release stamped at link time",
			stamped: "v1.4.0",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba"}},
			want:    "v1.4.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, get(tt.stamped, tt.info))
		})
	}
}

// TestVersionIsNeverEmpty covers the binary these tests were built as, since an empty version
// here would make a server that reports nothing compare equal to this build.
func TestVersionIsNeverEmpty(t *testing.T) {
	assert.NotEmpty(t, Version())
}
