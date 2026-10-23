package version

import (
	"regexp"
	"runtime/debug"
)

var stamped string

var stampedCommit string

const devel = "devel"

const shortCommit = 7

var release = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Version is which build this binary is.
func Version() string {
	info, _ := debug.ReadBuildInfo()

	return get(stamped, info)
}

// Commit is the commit this binary was built from, empty unless it names the source exactly.
func Commit() string {
	info, _ := debug.ReadBuildInfo()

	return getCommit(stampedCommit, info)
}

// Revision is the commit this binary was built from, set even when the tree held changes.
func Revision() string {
	if stampedCommit != "" {
		return stampedCommit
	}

	info, _ := debug.ReadBuildInfo()
	if info == nil {
		return ""
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}

	return ""
}

func get(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}

	version := devel
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}

	return version
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

// IsRelease reports whether a version is a tagged release.
func IsRelease(version string) bool {
	return release.MatchString(version)
}

// Short is the first seven characters of a build commit.
func Short(commit string) string {
	if len(commit) <= shortCommit {
		return commit
	}

	return commit[:shortCommit]
}
