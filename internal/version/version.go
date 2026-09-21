// Package version reports which build this binary is, from the tag stamped in at link time or,
// failing that, from the module information the toolchain left behind.
package version

import (
	"bytes"
	"errors"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"
)

var stamped string

var stampedCommit string

const devel = "devel"

const shortCommit = 7

// engineStampPrefix opens the version a deploy stamps on the engine when no release names
// the build, followed by the commit it was built from.
const engineStampPrefix = "v0.0.0-engine."

// buildIDMarker opens the build ID the linker writes near the start of every Go binary.
const buildIDMarker = "\xff Go build ID: \""

// buildIDWithin is how far into the binary the linker keeps that marker.
const buildIDWithin = 32 * 1024

const shortBuildID = 8

var selfBuildID = sync.OnceValue(readSelfBuildID)

// Version is which build this binary is.
func Version() string {
	info, _ := debug.ReadBuildInfo()

	return get(stamped, info, selfBuildID())
}

// Commit is the commit this binary was built from, empty unless it names the source exactly.
func Commit() string {
	info, _ := debug.ReadBuildInfo()

	return getCommit(stampedCommit, info)
}

func get(stamped string, info *debug.BuildInfo, buildID string) string {
	if stamped != "" {
		return stamped
	}

	version := devel
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}

	if buildID == "" || getCommit("", info) != "" {
		return version
	}

	if strings.Contains(version, "+") {
		return version + "." + buildID
	}

	return version + "+" + buildID
}

func getCommit(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}

	if info == nil {
		return ""
	}

	revision := ""

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				return ""
			}
		}
	}

	return revision
}

// Describe names a build, with the short commit beside the version when the build names one.
func Describe(version, commit string) string {
	if commit == "" {
		return version
	}

	return version + " (" + Short(commit) + ")"
}

// IsEngineStamp reports whether a version is a deploy's own stamp rather than a release.
func IsEngineStamp(version string) bool {
	return strings.HasPrefix(version, engineStampPrefix)
}

// Short is the first seven characters of a build commit.
func Short(commit string) string {
	if len(commit) <= shortCommit {
		return commit
	}

	return commit[:shortCommit]
}

func readSelfBuildID() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}

	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, buildIDWithin)

	read, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ""
	}

	return findBuildID(head[:read])
}

// findBuildID is the content hash the linker closed a binary's build ID with, shortened.
func findBuildID(head []byte) string {
	start := bytes.Index(head, []byte(buildIDMarker))
	if start < 0 {
		return ""
	}

	rest := head[start+len(buildIDMarker):]

	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}

	id := string(rest[:end])
	content := id[strings.LastIndex(id, "/")+1:]

	if len(content) > shortBuildID {
		content = content[:shortBuildID]
	}

	return content
}
