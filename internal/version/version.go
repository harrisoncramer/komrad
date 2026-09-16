// Package version reports which build this binary is, from the tag stamped in at link time or,
// failing that, from the module information the toolchain left behind.
package version

import "runtime/debug"

var stamped string

var stampedCommit string

const devel = "devel"

const shortCommit = 7

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

func get(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}

	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return devel
	}

	return info.Main.Version
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

	return version + " (" + short(commit) + ")"
}

func short(commit string) string {
	if len(commit) <= shortCommit {
		return commit
	}

	return commit[:shortCommit]
}
