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
	"komrad/lib/clock"
	"komrad/lib/xhttp"

	"github.com/spf13/cobra"
)

// controlPlaneTimeout bounds each call to the control plane's own endpoints.
const controlPlaneTimeout = 30 * time.Second

// defaultPollInterval is how many seconds to wait between polls when GitHub names no interval.
const defaultPollInterval = 5

func githubAuthorizeCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "authorize",
		Short: "Authorize GitHub for an engine behind a control plane",
		Long: `Authorize GitHub for an engine behind a control plane.

This prints a code and a GitHub address. Enter the code there, and the control plane keeps
the GitHub login the forge proxy adds to the engine's GitHub requests. It reaches the control
plane at the engine's url, signed in as you, and needs your grant on the engine.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return authorizeGitHub(cmd.Context(), state, cmd.OutOrStdout())
		},
	}
}

func authorizeGitHub(ctx context.Context, state *app, out io.Writer) error {
	if state.env.URL == "" {
		return errors.New("only an engine behind a control plane authorizes GitHub, so name one with --engine")
	}

	client := xhttp.NewClientOver(state.signTransport(xhttp.NewTransport()), controlPlaneTimeout)

	var code controlplane.DeviceCode
	if err := postControlPlane(ctx, client, state.env.URL+controlplane.DevicePath, &code); err != nil {
		return err
	}

	fmt.Fprintf(out, "Enter %s at %s\n", code.UserCode, code.VerificationURI) //nolint:errcheck // a closed stdout is the caller going away

	interval := code.Interval
	for {
		clock.Sleep(ctx, pollInterval(interval))
		if err := ctx.Err(); err != nil {
			return err
		}

		var poll controlplane.DevicePoll
		if err := postControlPlane(ctx, client, state.env.URL+controlplane.DevicePollPath, &poll); err != nil {
			return err
		}

		isAuthorized, err := readDevicePoll(poll)
		if err != nil {
			return err
		}
		if isAuthorized {
			fmt.Fprintln(out, "GitHub is authorized for this engine.") //nolint:errcheck // a closed stdout is the caller going away

			return nil
		}

		interval = poll.Interval
	}
}

// pollInterval is how long to wait before polling the device flow, given GitHub's interval in
// seconds.
func pollInterval(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultPollInterval
	}

	return time.Duration(seconds) * time.Second
}

// readDevicePoll reports whether a poll says GitHub is authorized, and fails when the code ran out
// or the person turned it down.
func readDevicePoll(poll controlplane.DevicePoll) (bool, error) {
	switch poll.Status {
	case controlplane.DeviceStatusAuthorized:
		return true, nil
	case controlplane.DeviceStatusExpired:
		return false, errors.New("the code ran out before it was entered")
	case controlplane.DeviceStatusDenied:
		return false, errors.New("the authorization was turned down at GitHub")
	case controlplane.DeviceStatusPending:
	}

	return false, nil
}

// controlPlaneMessage is the control plane's answer to a start or a stop.
type controlPlaneMessage struct {
	Message string `json:"message"`
}

// postControlPlane asks one of the control plane's own endpoints to act and reads its answer.
func postControlPlane(ctx context.Context, client *http.Client, endpoint string, answer any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to build a request for %s: %w", endpoint, err)
	}

	return callControlPlane(client, req, answer)
}

// getControlPlane reads one of the control plane's own endpoints.
func getControlPlane(ctx context.Context, client *http.Client, endpoint string, answer any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to build a request for %s: %w", endpoint, err)
	}

	return callControlPlane(client, req, answer)
}

func callControlPlane(client *http.Client, req *http.Request, answer any) error {
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach the control plane: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("the control plane answered %s: %s", res.Status, readReason(res.Body))
	}

	if err := json.NewDecoder(io.LimitReader(res.Body, maxReason)).Decode(answer); err != nil {
		return fmt.Errorf("failed to read the control plane's answer: %w", err)
	}

	return nil
}
