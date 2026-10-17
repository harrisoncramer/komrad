package postgres

// Version is the release of the embedded Postgres a build made with the postgres tag carries.
const Version = "18.6.0"

// IsEmbedded reports whether this build carries the embedded Postgres.
func IsEmbedded() bool {
	return len(bundle) > 0
}
