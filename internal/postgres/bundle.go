package postgres

// Version is the Postgres release a build made with the postgres tag carries.
const Version = "18.6.0"

// IsBundled reports whether this build carries a Postgres of its own.
func IsBundled() bool {
	return len(bundle) > 0
}
