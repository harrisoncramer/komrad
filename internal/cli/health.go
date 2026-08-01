package cli

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthPath is where the service answers whether it can do its job. It is asked before
// the panel starts talking to a server, so the two agree on what "up" means.
const HealthPath = "/healthz"

// healthTimeout bounds the check. It is short because the answer is wanted by something
// deciding whether to wait, and a database that takes this long to answer a ping is not
// one the panel should be started against.
const healthTimeout = 2 * time.Second

// newHealthHandler answers whether the service can serve. It reaches the database rather
// than reporting that the process is running, because a process that is listening and
// cannot reach its store fails every call it is given: a socket that accepts is exactly
// what an unusable server looks like from outside.
//
// It is mounted outside the logging middleware. A readiness check runs on a loop by
// definition, and a log where every second is a health check is a log nobody reads. What
// it would have said is on the record anyway, since a failing check means a failing
// database and the loops are already writing that.
func newHealthHandler(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			// The reason goes in the body rather than only in the status, so whoever is
			// polling can say why the wait is failing instead of only that it is.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unreachable: " + err.Error()))

			return
		}

		_, _ = w.Write([]byte("ok"))
	})
}
