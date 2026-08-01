package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// StopPath is where the service is asked to stop. It is a plain route rather than an RPC
// so that it cannot be reached as a tool: the MCP surface is built by walking the protos
// for a tool annotation, and a mux route is not in that list however the services grow.
const StopPath = "/stop"

// stopWaitFor is how long the stop command waits for the process to go. It is longer than
// every stage of serve's own teardown put together, since the point of asking rather than
// signalling is to find out what happened.
const stopWaitFor = 30 * time.Second

// stopPollInterval is how often the address is retried while waiting.
const stopPollInterval = 100 * time.Millisecond

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
		_, _ = fmt.Fprint(w, os.Getpid())
	})
}

// stopCmd asks a running server to stop and waits for it to be gone.
//
// It exists so a reload can ask rather than signal. A signal says nothing back, which
// leaves whoever sent it to guess at how long to wait and then kill on a timer, and
// guessing short is what abandons a launch partway through.
func stopCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "stop",
		Short:         "Ask the running server to stop and wait for it",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr := state.env.Addr

			pid, err := stopServer(addr)
			if err != nil {
				return err
			}

			if pid == 0 {
				cmd.Printf("nothing is serving at %s\n", addr)

				return nil
			}

			cmd.Printf("the server at %s has stopped\n", addr)

			return nil
		},
	}
}

// stopServer asks the server at addr to stop and waits for its process to be gone, returning
// the pid it stopped and zero when nothing was serving.
func stopServer(addr string) (int, error) {
	pid, err := requestStop(addr)
	if err != nil {
		return 0, err
	}

	if pid == 0 {
		return 0, nil
	}

	if err := awaitStopped(pid, stopWaitFor); err != nil {
		return 0, err
	}

	return pid, nil
}

// requestStop asks the server at addr to stop and returns the pid it answered with, zero
// when nothing was there to ask. A connection that is refused means the work is already
// done, which is a success rather than a failure to report to whoever is reloading.
func requestStop(addr string) (int, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	res, err := client.Post("http://"+addr+StopPath, "text/plain", nil)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return 0, nil
		}

		return 0, fmt.Errorf("failed to ask the server at %s to stop: %w", addr, err)
	}
	defer func() { _ = res.Body.Close() }()

	// A server too old to hold this route answers 404, which has to be said plainly. It
	// is still running afterwards, and reporting success would have a reload carry on and
	// respawn a panel against the process it meant to replace.
	if res.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("the server at %s would not stop: %s", addr, res.Status)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read what the server at %s answered: %w", addr, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		return 0, fmt.Errorf("the server at %s did not say which process it is: %w", addr, err)
	}

	return pid, nil
}

// awaitStopped waits for the process to be gone. It watches the process rather than the
// address because the listener closes at the top of the teardown, so an address that has
// stopped answering says only that the shutdown has begun.
func awaitStopped(pid int, within time.Duration) error {
	deadline := time.Now().Add(within)

	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}

		time.Sleep(stopPollInterval)
	}

	return fmt.Errorf("the server was asked to stop and process %d is still running after %s", pid, within)
}

// processAlive reports whether a process still exists. Signal zero is delivered to nothing
// and only reports whether it could have been.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return process.Signal(syscall.Signal(0)) == nil
}
