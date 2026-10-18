package postgres

import "strings"

// Version is the release of the embedded Postgres a build made with the postgres tag carries.
const Version = "18.6"

// getMajorVersion is the major release of Version, which a cluster's files are tied to.
func getMajorVersion() string {
	major, _, _ := strings.Cut(Version, ".")

	return major
}

// IsEmbedded reports whether this build carries the embedded Postgres.
func IsEmbedded() bool {
	return len(bundle) > 0
}
