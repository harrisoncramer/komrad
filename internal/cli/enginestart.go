package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"komrad/internal/controlplane"
	"komrad/internal/server"
	"komrad/lib/clock"
	"komrad/lib/xhttp"

	"github.com/spf13/cobra"
)

// engineStartOrStopWait bounds how long komrad engine start and komrad engine stop wait for the engine.
const engineStartOrStopWait = 5 * time.Minute

// engineStartOrStopPoll is how often they ask the engine's health path.
const engineStartOrStopPoll = 2 * time.Second

// startOrStop is one of the two things the control plane does to an engine, and the answer
// that says it is done.
type startOrStop struct {
	Path string
	// Reached reports whether an answer to the engine's health path, by its status code and the
	// status it carries, says the engine is running or stopped as asked.
	Reached func(status int, health string) bool
	Done    string
	Timeout string
}

var engineStart = startOrStop{
	Path:    controlplane.StartPath,
	Reached: func(status int, _ string) bool { return status == http.StatusOK },
	Done:    "The engine is running.",
	Timeout: "the engine did not start within five minutes",
}

var engineStop = startOrStop{
	Path:    controlplane.StopPath,
	Reached: func(_ int, health string) bool { return health == server.HealthStopped },
	Done:    "The engine is stopped.",
	Timeout: "the engine did not stop within five minutes",
}

func engineStartCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start an engine behind a control plane",
		Long: `Start an engine behind a control plane.

This asks the control plane at the engine's url to start the engine, signed in as you, and
needs your grant on it. It waits up to five minutes for the engine itself to answer.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStartOrStop(cmd, state, engineStart)
		},
	}
}

func engineStopCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop an engine behind a control plane",
		Long: `Stop an engine behind a control plane.

This asks the control plane at the engine's url to stop the engine, signed in as you, and
needs your grant on it. It waits up to five minutes for the control plane to say the engine
is stopped.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStartOrStop(cmd, state, engineStop)
		},
	}
}

func runStartOrStop(cmd *cobra.Command, state *app, asked startOrStop) error {
	if state.env.URL == "" {
		return errors.New("only an engine behind a control plane starts and stops this way, so name one with --engine")
	}

	return startOrStopEngine(cmd.Context(), startOrStopEngineParams{
		URL:       state.env.URL,
		Transport: state.signTransport(xhttp.NewTransport()),
		Sleeper:   clock.Wall{},
		Asked:     asked,
		Out:       cmd.OutOrStdout(),
	})
}

type startOrStopEngineParams struct {
	URL       string
	Transport http.RoundTripper
	Sleeper   clock.Sleeper
	Asked     startOrStop
	Out       io.Writer
}

// startOrStopEngine asks the control plane to start or stop the engine, then asks the engine's
// health path until the answer says it is running or stopped.
func startOrStopEngine(ctx context.Context, params startOrStopEngineParams) error {
	var accepted struct {
		Message string `json:"message"`
	}
	if err := postControlPlane(ctx, xhttp.NewClientOver(params.Transport, controlPlaneTimeout), params.URL+params.Asked.Path, &accepted); err != nil {
		return err
	}

	for range engineStartOrStopWait / engineStartOrStopPoll {
		if status, health := readHealth(ctx, params.Transport, params.URL); params.Asked.Reached(status, health) {
			fmt.Fprintln(params.Out, params.Asked.Done) //nolint:errcheck // a closed stdout is the caller going away

			return nil
		}

		params.Sleeper.Sleep(ctx, engineStartOrStopPoll)
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	return errors.New(params.Asked.Timeout)
}

// readHealth is the status code the engine's url answers its health path with and the status
// the answer carries, or zero and empty when nothing answers.
func readHealth(ctx context.Context, transport http.RoundTripper, base string) (int, string) {
	res, err := healthCheck(ctx, transport, base)
	if err != nil {
		return 0, ""
	}
	defer func() { _ = res.Body.Close() }()

	var health server.Health
	_ = json.NewDecoder(io.LimitReader(res.Body, maxReason)).Decode(&health) //nolint:errcheck,errchkjson // an answer that is not a health body carries no status

	return res.StatusCode, health.Status
}
