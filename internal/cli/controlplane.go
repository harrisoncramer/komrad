package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"komrad/internal/controlplane"
	"komrad/internal/forge"
	"komrad/internal/interceptors"
	"komrad/internal/logging"
	"komrad/internal/store"
	"komrad/lib/xhttp"

	"github.com/spf13/cobra"
)

// deviceFlowTimeout bounds each call the control plane makes to GitHub's device flow endpoints.
const deviceFlowTimeout = 30 * time.Second

// controlPlaneLoginsDir is where the control plane keeps each engine's GitHub login under its
// state directory.
const controlPlaneLoginsDir = "control-plane"

func controlPlaneCmd(state *app) *cobra.Command {
	controlPlane := &cobra.Command{
		Use:         "control-plane",
		Annotations: map[string]string{needsEmbeddedPostgres: ""},
		Short:       "Run the control plane in front of a team's engines",
		Hidden:      true,
	}

	controlPlane.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Serve the forge proxy and the control plane's endpoints",
		Long: `Serve the forge proxy and the control plane's endpoints.

The forge proxy listens on control_plane.forge_proxy_addr for the engines control_plane.engines
names, and sends their GitHub requests on with the GitHub login each engine's person authorized.
The control plane's endpoints listen on control_plane.addr, where a person signed in through
[sign_in] and holding an engine's grant authorizes GitHub for it. It reads grants as
owner_database_url.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := shutdownSignal(cmd.Context())
			defer stop()

			return serveControlPlane(ctx, state)
		},
	})

	return controlPlane
}

func serveControlPlane(ctx context.Context, state *app) error {
	settings := state.controlPlane
	if settings.CACertFile == "" || settings.GitHubClientID == "" || !state.signIn.IsConfigured() {
		return errors.New("the control plane needs control_plane.ca_cert_file, control_plane.ca_key_file, control_plane.github_client_id and [sign_in]")
	}

	level, err := logging.ParseLevel(state.logLevel)
	if err != nil {
		return err
	}
	logger := logging.New(os.Stderr, level).With(slog.String(logging.ProcessName, logging.ProcessControlPlane))

	engines, err := settings.GetEngines()
	if err != nil {
		return fmt.Errorf("failed to read control_plane.engines: %w", err)
	}

	ca, err := controlplane.LoadCA(settings.CACertFile, settings.CAKeyFile)
	if err != nil {
		return err
	}

	if err := startEmbeddedPostgres(ctx, getEmbeddedPostgresDir(state.env)); err != nil {
		return err
	}

	grants, err := store.OpenGrantStore(ctx, state.env.OwnerDatabaseURL)
	if err != nil {
		return err
	}
	defer closeWithin(logger, "the grants", grants.Close)

	upstream := xhttp.NewTransport()
	upstream.Proxy = nil

	plane := controlplane.New(controlplane.NewParams{
		Logger:  logger,
		Engines: engines,
		CA:      ca,
		Logins:  forge.NewFileStore(filepath.Join(state.env.StateDir, controlPlaneLoginsDir)),
		Grants:  grants,
		Verifier: interceptors.NewProviderVerifier(interceptors.NewProviderVerifierParams{
			Issuer:   state.signIn.Issuer,
			ClientID: state.signIn.ClientID,
		}),
		GitHubClientID: settings.GitHubClientID,
		GitHub:         controlplane.GitHubURL,
		Client:         xhttp.NewClientOver(upstream, deviceFlowTimeout),
		Upstream:       upstream,
		Now:            time.Now,
	})
	defer func() {
		if err := plane.Close(); err != nil {
			logger.LogAttrs(ctx, slog.LevelWarn, "the forge proxy's tunnels did not close cleanly", logging.Err(err)...)
		}
	}()

	endpoints, err := (&net.ListenConfig{}).Listen(ctx, "tcp", settings.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", settings.Addr, err)
	}

	proxy, err := (&net.ListenConfig{}).Listen(ctx, "tcp", settings.ForgeProxyAddr)
	if err != nil {
		_ = endpoints.Close() //nolint:errcheck // the failed listen is what the caller hears about

		return fmt.Errorf("failed to listen for engines on %s: %w", settings.ForgeProxyAddr, err)
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "the control plane is listening",
		slog.String(logging.ServerAddress, endpoints.Addr().String()))
	logger.LogAttrs(ctx, slog.LevelInfo, "the forge proxy is listening",
		slog.String(logging.ServerAddress, proxy.Addr().String()))

	served := make(chan error, 2)
	servers := []*http.Server{
		startHTTPServer(ctx, startHTTPServerParams{Logger: logger, Handler: plane.Handler(), Listener: endpoints, Served: served}),
		startHTTPServer(ctx, startHTTPServerParams{Logger: logger, Handler: plane.ForgeProxy(), Listener: proxy, Served: served}),
	}

	var failure error
	select {
	case failure = <-served:
	case <-ctx.Done():
		logger.LogAttrs(ctx, slog.LevelInfo, "the control plane is stopping")
	}

	drain, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), httpDrainTimeout)
	defer cancelDrain()

	for _, server := range servers {
		if err := server.Shutdown(drain); err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "the control plane would not shut down cleanly", logging.Err(err)...)
		}
	}

	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}

	return failure
}
