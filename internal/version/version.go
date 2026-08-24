package version

import "runtime/debug"

var stamped string

const devel = "devel"

// Version is which build this binary is.
func Version() string {
	info, _ := debug.ReadBuildInfo()

	return get(stamped, info)
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
