package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"komrad/internal/logging"
	"komrad/internal/version"

	"github.com/jackc/pgx/v5/pgxpool"
)

const HealthPath = "/healthz"

const healthTimeout = 2 * time.Second

type Health struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Revision string `json:"revision,omitempty"`
	Error    string `json:"error,omitempty"`
}

func healthBody(pingErr error) (Health, int) {
	if pingErr != nil {
		return Health{
			Status:   "unavailable",
			Version:  version.Version(),
			Commit:   version.Commit(),
			Revision: version.Revision(),
			Error:    "database unreachable: " + pingErr.Error(),
		}, http.StatusServiceUnavailable
	}

	return Health{
		Status:   "ok",
		Version:  version.Version(),
		Commit:   version.Commit(),
		Revision: version.Revision(),
	}, http.StatusOK
}

func newHealthHandler(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()

		body, status := healthBody(pool.Ping(ctx))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to write the health body", logging.Err(err)...)
		}
	})
}
