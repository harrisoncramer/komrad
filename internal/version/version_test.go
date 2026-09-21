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
	const buildID = "zGcGo0vH"

	exact := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "3b6495f546ba0403648b090dfa7695d160efea18"},
		{Key: "vcs.modified", Value: "false"},
	}

	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		buildID string
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
			name:    "a build from a worktree, which the toolchain records nothing for",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			buildID: buildID,
			want:    "devel+zGcGo0vH",
		},
		{
			name:    "a build from a dirty tree",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba+dirty"}},
			want:    "v0.0.0-20260801021359-3b6495f546ba+dirty",
		},
		{
			name:    "a build from a dirty tree, told apart from the last one by the binary itself",
			stamped: "",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba+dirty"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "3b6495f546ba0403648b090dfa7695d160efea18"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			buildID: buildID,
			want:    "v0.0.0-20260801021359-3b6495f546ba+dirty.zGcGo0vH",
		},
		{
			name:    "an ordinary go build in a checkout",
			stamped: "",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba"}, Settings: exact},
			buildID: buildID,
			want:    "v0.0.0-20260801021359-3b6495f546ba",
		},
		{
			name:    "a release stamped at link time",
			stamped: "v1.4.0",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260801021359-3b6495f546ba"}},
			buildID: buildID,
			want:    "v1.4.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, get(tt.stamped, tt.info, tt.buildID))
		})
	}
}

// TestFindBuildID covers reading the linker's build ID off the head of a binary.
func TestFindBuildID(t *testing.T) {
	tests := []struct {
		name string
		head string
		want string
	}{
		{
			name: "a file that is not a Go binary",
			head: "#!/bin/sh\necho hi\n",
			want: "",
		},
		{
			name: "a marker cut off before the id closes",
			head: "\x00\xff Go build ID: \"Dg7XN2bd6cklxz4Lz8Q_/zVN9yjIwSr8K",
			want: "",
		},
		{
			name: "a linked binary, shortened to its content hash",
			head: "\x00\x00\xff Go build ID: \"Dg7XN2bd6cklxz4Lz8Q_/zVN9yjIwSr8KpmxcG6QG/3j2z3IK9mDgx9R_yP3FH/zGcGo0vHRRzr0braXzqy\"\x00\xcc",
			want: "zGcGo0vH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findBuildID([]byte(tt.head)))
		})
	}
}

// TestSelfBuildIDIsRead covers the binary these tests were built as.
func TestSelfBuildIDIsRead(t *testing.T) {
	assert.Len(t, selfBuildID(), shortBuildID)
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
