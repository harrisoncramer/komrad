package doctor

import (
	"context"

	"komrad/internal/postgres"
)

const (
	fixNoEmbeddedPostgres        = "install a release binary, or build with mise run go:build"
	fixNoEmbeddedPostgresCluster = "move your config file aside, then run komrad setup"
)

var (
	errEmbeddedPostgresDown = &problemError{
		Code:    "embedded_postgres_down",
		Message: "the embedded Postgres is not running",
	}
	errNoEmbeddedPostgres = &problemError{
		Code:    "no_embedded_postgres",
		Message: "this build of komrad carries no embedded Postgres",
	}
	errNoEmbeddedPostgresCluster = &problemError{
		Code:    "no_embedded_postgres_cluster",
		Message: "the config turns on the embedded Postgres, but none is set up under the state directory",
	}
)

// newEmbeddedPostgresCheck asserts the embedded Postgres under the state directory is up, and that
// this build carries one to start it with.
func newEmbeddedPostgresCheck() Check {
	return Check{
		Name:        CheckEmbeddedPostgres,
		Description: "the embedded Postgres is running, and komrad start brings it up",
		Run: func(ctx context.Context, env Env) Result {
			dir := postgres.GetDir(env.StateDir)
			running := env.IsEmbeddedPostgresRunning(ctx, dir)

			switch {
			case !running && env.EmbeddedPostgresVersion == "":
				return fail(errNoEmbeddedPostgres, dir, fixNoEmbeddedPostgres)
			case !running && !env.HasEmbeddedPostgresCluster(dir):
				return fail(errNoEmbeddedPostgresCluster, dir, fixNoEmbeddedPostgresCluster)
			case !running:
				return fail(errEmbeddedPostgresDown, dir, "komrad start")
			case env.EmbeddedPostgresVersion == "":
				return warn(errNoEmbeddedPostgres, dir)
			}

			return pass(env.EmbeddedPostgresVersion + " in " + dir)
		},
	}
}
