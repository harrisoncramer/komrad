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

// TestVersionIsNeverEmpty covers the binary these tests were built as.
func TestVersionIsNeverEmpty(t *testing.T) {
	assert.NotEmpty(t, Version())
}

// TestGetCommit covers the commit the panel compares a server against, which has to be empty
// rather than wrong whenever it does not name the source exactly.
func TestGetCommit(t *testing.T) {
	revision := debug.BuildSetting{Key: "vcs.revision", Value: "6da018be0403648b090dfa7695d160efea180b49"}

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
			want:    "",
		},
		{
			name:    "a build the toolchain recorded no revision for",
			stamped: "",
			info:    &debug.BuildInfo{},
			want:    "",
		},
		{
			name:    "an ordinary go build in a checkout",
			stamped: "",
			info: &debug.BuildInfo{Settings: []debug.BuildSetting{
				revision,
				{Key: "vcs.modified", Value: "false"},
			}},
			want: "6da018be0403648b090dfa7695d160efea180b49",
		},
		{
			name:    "a build from a dirty tree, which the revision no longer describes",
			stamped: "",
			info: &debug.BuildInfo{Settings: []debug.BuildSetting{
				revision,
				{Key: "vcs.modified", Value: "true"},
			}},
			want: "",
		},
		{
			name:    "a cross build stamped at link time",
			stamped: "0000000000000000000000000000000000000000",
			info:    &debug.BuildInfo{Settings: []debug.BuildSetting{revision}},
			want:    "0000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, getCommit(tt.stamped, tt.info))
		})
	}
}
