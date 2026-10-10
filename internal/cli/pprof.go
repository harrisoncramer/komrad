package cli

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"time"

	"komrad/config"
	"komrad/internal/logging"
)

func startPprof(ctx context.Context, logger *slog.Logger, settings config.Profiler) *http.Server {
	if !settings.Enabled {
		return nil
	}

	pprofServer := &http.Server{
		Addr:              settings.Address,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		defer logging.LogAndRepanic(ctx)

		if err := pprofServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.LogAttrs(ctx, slog.LevelError, "the pprof server stopped", logging.Err(err)...)
		}
	}()

	return pprofServer
}
