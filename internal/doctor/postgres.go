package doctor

import (
	"context"

	"komrad/internal/postgres"
)

const (
	fixPostgresNotBundled = "install a release binary, or build with mise run go:build"
	fixNoPostgresCluster  = "move your config file aside, then run komrad setup"
)

var (
	errPostgresDown = &problemError{
		Code:    "postgres_down",
		Message: "the Postgres komrad runs on this machine is not running",
	}
	errPostgresNotBundled = &problemError{
		Code:    "postgres_not_bundled",
		Message: "this build of komrad carries no Postgres, so it cannot start the one komrad runs",
	}
	errNoPostgresCluster = &problemError{
		Code:    "postgres_no_cluster",
		Message: "the config says komrad runs Postgres here, but none is set up under the state directory",
	}
)

// newPostgresCheck asserts the Postgres komrad runs under its state directory is up, and that
// this build carries one to start it with.
func newPostgresCheck() Check {
	return Check{
		Name:        CheckPostgres,
		Description: "komrad runs its own Postgres on this machine, and komrad start brings it up",
		Run: func(ctx context.Context, env Env) Result {
			dir := postgres.Dir(env.StateDir)
			running := env.IsPostgresRunning(ctx, dir)

			switch {
			case !running && env.PostgresVersion == "":
				return fail(errPostgresNotBundled, dir, fixPostgresNotBundled)
			case !running && !env.HasPostgresCluster(dir):
				return fail(errNoPostgresCluster, dir, fixNoPostgresCluster)
			case !running:
				return fail(errPostgresDown, dir, "komrad start")
			case env.PostgresVersion == "":
				return warn(errPostgresNotBundled, dir)
			}

			return pass(env.PostgresVersion + " in " + dir)
		},
	}
}
