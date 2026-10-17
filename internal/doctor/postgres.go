package doctor

import (
	"context"

	"komrad/internal/postgres"
)

var errPostgresDown = &problemError{
	Code:    "postgres_down",
	Message: "the Postgres komrad runs on this machine is not running",
}

// newPostgresCheck asserts the Postgres komrad runs under its state directory is up.
func newPostgresCheck() Check {
	return Check{
		Name:        "postgres",
		Description: "komrad runs its own Postgres on this machine, and komrad start brings it up",
		Run: func(ctx context.Context, env Env) Result {
			dir := postgres.Dir(env.StateDir)
			if !env.IsPostgresRunning(ctx, dir) {
				return fail(errPostgresDown, dir, "komrad start")
			}

			return pass(dir)
		},
	}
}
