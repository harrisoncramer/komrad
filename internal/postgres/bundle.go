package postgres

// Version is the Postgres release a build made with the postgres tag carries.
const Version = "18.6.0"

// Bundled reports whether this build carries a Postgres of its own.
func Bundled() bool {
	return len(bundle) > 0
}
