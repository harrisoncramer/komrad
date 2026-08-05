package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"weave/internal/logging"
	"weave/internal/version"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthPath is where the service answers whether it can do its job, and which build it is.
const HealthPath = "/healthz"

// healthTimeout bounds the check, since a database this slow to answer a ping is not one to
// start the panel against.
const healthTimeout = 2 * time.Second

// Health is what the health route answers.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// healthBody is the answer and the status to send it with, carrying the version either way,
// since a stale build whose database is down still has to be replaced rather than waited for.
func healthBody(pingErr error) (Health, int) {
	if pingErr != nil {
		return Health{
			Status:  "unavailable",
			Version: version.Version(),
			Error:   "database unreachable: " + pingErr.Error(),
		}, http.StatusServiceUnavailable
	}

	return Health{Status: "ok", Version: version.Version()}, http.StatusOK
}

// newHealthHandler answers whether the service can serve, reaching the database rather than
// reporting that the process is running, since a socket that accepts is what an unusable
// server looks like from outside.
func newHealthHandler(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()

		body, status := healthBody(pool.Ping(ctx))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			// The status is already on the wire, so this is the caller having gone away.
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to write the health body", logging.Err(err)...)
		}
	})
}
