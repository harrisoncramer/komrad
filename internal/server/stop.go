package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"komrad/internal/logging"
)

const StopPath = "/stop"

func newStopHandler(requested chan<- struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "the stop route takes a POST", http.StatusMethodNotAllowed)

			return
		}

		select {
		case requested <- struct{}{}:
		default:
		}

		w.WriteHeader(http.StatusAccepted)
		if _, err := fmt.Fprint(w, os.Getpid()); err != nil {
			ctx := r.Context()
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to write the pid to a stop request", logging.Err(err)...)
		}
	})
}
