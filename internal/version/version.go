// Package version reports which build this binary is, so the panel can tell whether the
// server already on its address came from the same one.
package version

import "runtime/debug"

// stamped is set with -ldflags -X for a release build and is empty for every other one.
var stamped string

// devel is what a build the toolchain recorded nothing about reports, so a version is never
// empty and no build can be mistaken for one that cannot say.
const devel = "devel"

// Version is which build this binary is.
func Version() string {
	info, _ := debug.ReadBuildInfo()

	return get(stamped, info)
}

// get prefers a release stamp, then what the toolchain recorded, which is a pseudo-version
// off the commit for an ordinary build and the tag for a go install.
func get(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}

	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return devel
	}

	return info.Main.Version
}
