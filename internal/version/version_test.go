package version

import (
	"encoding/binary"
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

// TestFindMarkedBuildID covers reading the linker's build ID off the head of a binary that
// carries it in the text.
func TestFindMarkedBuildID(t *testing.T) {
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
			name: "a linked binary",
			head: "\x00\x00\xff Go build ID: \"Dg7XN2bd6cklxz4Lz8Q_/zVN9yjIwSr8KpmxcG6QG/3j2z3IK9mDgx9R_yP3FH/zGcGo0vHRRzr0braXzqy\"\x00\xcc",
			want: "Dg7XN2bd6cklxz4Lz8Q_/zVN9yjIwSr8KpmxcG6QG/3j2z3IK9mDgx9R_yP3FH/zGcGo0vHRRzr0braXzqy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findMarkedBuildID([]byte(tt.head)))
		})
	}
}

// TestFindNotedBuildID covers reading the build ID out of the note an ELF binary carries it in.
func TestFindNotedBuildID(t *testing.T) {
	const id = "Dg7XN2bd6cklxz4Lz8Q_/zVN9yjIwSr8KpmxcG6QG/3j2z3IK9mDgx9R_yP3FH/zGcGo0vHRRzr0braXzqy"

	note := func(nameLen, descLen uint32, body string) []byte {
		header := make([]byte, 0, noteHeaderLen+len(body))
		header = binary.LittleEndian.AppendUint32(header, nameLen)
		header = binary.LittleEndian.AppendUint32(header, descLen)
		header = binary.LittleEndian.AppendUint32(header, 4)

		return append(header, body...)
	}

	tests := []struct {
		name string
		note []byte
		want string
	}{
		{
			name: "a section too short to hold a header",
			note: []byte{0, 0, 0},
			want: "",
		},
		{
			name: "a note whose descriptor runs past the section",
			note: note(4, 200, "Go\x00\x00"+id),
			want: "",
		},
		{
			name: "the note the linker writes",
			note: note(4, uint32(len(id)), "Go\x00\x00"+id),
			want: id,
		},
		{
			name: "a name padded out to four bytes",
			note: note(3, uint32(len(id)), "Go\x00\x00"+id),
			want: id,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findNotedBuildID(tt.note, binary.LittleEndian))
		})
	}
}

// TestShortenBuildID covers the part of a build ID a version carries.
func TestShortenBuildID(t *testing.T) {
	assert.Equal(t, "zGcGo0vH", shortenBuildID("Dg7XN2bd6cklxz4Lz8Q_/zVN9yjIwSr8KpmxcG6QG/3j2z3IK9mDgx9R_yP3FH/zGcGo0vHRRzr0braXzqy"))
	assert.Equal(t, "abc", shortenBuildID("abc"))
	assert.Empty(t, shortenBuildID(""))
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
		{name: "a tagged release", version: "v1.4.0", want: true},
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
