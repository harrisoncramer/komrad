package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"weave/internal/logging"
)

// StopPath is where the service is asked to stop. It is a plain route rather than an RPC
// so that it cannot be reached as a tool: the MCP surface is built by walking the protos
// for a tool annotation, and a mux route is not in that list however the services grow.
const StopPath = "/stop"

// newStopHandler answers a request to stop by asking for one and saying so, rather than
// stopping while the caller holds the connection. Shutdown waits for the requests in
// flight and this is one of them, so tearing down from in here would wait on itself.
//
// It refuses anything but POST. A route that stops the service is not one for anything
// wandering over the address to reach with a GET.
func newStopHandler(requested chan<- struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "the stop route takes a POST", http.StatusMethodNotAllowed)

			return
		}

		// Asked for once. A second request while the first is being acted on is answered
		// the same way rather than blocking on a channel nothing is reading any more.
		select {
		case requested <- struct{}{}:
		default:
		}

		// The pid goes back because the listener closes at the top of the teardown, well
		// before the process is gone. A caller watching the address would be told the
		// server had stopped while it was still draining, which is the guess that asking
		// rather than signalling exists to avoid.
		w.WriteHeader(http.StatusAccepted)
		if _, err := fmt.Fprint(w, os.Getpid()); err != nil {
			// The status is already on the wire, so this is the caller having gone away.
			ctx := r.Context()
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to write the pid to a stop request", logging.Err(err)...)
		}
	})
}
