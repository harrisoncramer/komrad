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
		Message: "the embedded Postgres is not running",
	}
	errPostgresNotBundled = &problemError{
		Code:    "postgres_not_bundled",
		Message: "this build of komrad carries no embedded Postgres",
	}
	errNoPostgresCluster = &problemError{
		Code:    "postgres_no_cluster",
		Message: "the config turns on the embedded Postgres, but none is set up under the state directory",
	}
)

// newPostgresCheck asserts the embedded Postgres under the state directory is up, and that
// this build carries one to start it with.
func newPostgresCheck() Check {
	return Check{
		Name:        CheckPostgres,
		Description: "the embedded Postgres is running, and komrad start brings it up",
		Run: func(ctx context.Context, env Env) Result {
			dir := postgres.GetDir(env.StateDir)
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
