package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGet covers what each way of building this binary reports.
func TestGet(t *testing.T) {
	exact := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "3b6495f546ba0403648b090dfa7695d160efea18"},
		{Key: "vcs.modified", Value: "false"},
	}

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
			name:    "a build from a dirty tree",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba+dirty"}},
			want:    "v0.0.0-20260801021359-3b6495f546ba+dirty",
		},
		{
			name:    "an ordinary go build in a checkout",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba"}, Settings: exact},
			want:    "v0.0.0-20260801021359-3b6495f546ba",
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

// TestGetCommit covers the commit a build names, which has to be empty rather than wrong whenever it does not name the source exactly.
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

// TestDescribe covers naming a build where the version alone cannot.
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

// TestIsRelease covers telling a tagged release from every other build.
func TestIsRelease(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{
			name:    "a tagged release",
			version: "v1.4.0",
			want:    true,
		},
		{name: "a dirty local build", version: "v0.0.0-local.e5bf761e3+dirty.204554"},
		{name: "an engine stamp", version: "v0.0.0-engine.6da018b"},
		{name: "a pseudo version", version: "v0.0.0-20260916203018-6da018be0403"},
		{name: "a devel build", version: "devel"},
		{name: "a release candidate", version: "v1.4.0-rc.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsRelease(tt.version))
		})
	}
}
