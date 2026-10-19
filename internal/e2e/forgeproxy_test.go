//go:build integration

package e2e

import (
	"log/slog"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/controlplane"
	"komrad/internal/controlplanetest"
	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/interceptors"
	"komrad/internal/komradtest"
	"komrad/internal/store"
	terminalv1 "komrad/proto/gen/terminal/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestIntegrationForgeProxySendsEachEngineToGitHubAsItsOwnPerson covers two engines behind one
// control plane, each reaching GitHub from the server and from gh as the person it belongs to.
func TestIntegrationForgeProxySendsEachEngineToGitHubAsItsOwnPerson(t *testing.T) {
	ca := controlplanetest.NewCA(t)
	github := controlplanetest.StartGitHub(t, ca)

	logins := forge.NewFileStore(t.TempDir())
	require.NoError(t, logins.Set(t.Context(), "harry", forge.NewSecret("gho_harry")))
	require.NoError(t, logins.Set(t.Context(), "ada", forge.NewSecret("gho_ada")))

	grants, err := store.OpenGrantStore(t.Context(), dbtest.New(t).OwnerURL)
	require.NoError(t, err)
	t.Cleanup(grants.Close)

	engines := map[string]string{"harry": "credential-harry", "ada": "credential-ada"}
	served := controlplane.New(controlplane.NewParams{
		Logger:         slog.New(slog.DiscardHandler),
		Engines:        engines,
		CA:             ca.Cert,
		Logins:         logins,
		Grants:         grants,
		Verifier:       interceptors.NewProviderVerifier(interceptors.NewProviderVerifierParams{Issuer: "https://idp.example.com", ClientID: "komrad"}),
		GitHubClientID: "Iv1.komrad",
		GitHub:         github.URL,
		Client:         github.Client(),
		Upstream:       github.Upstream(),
		Now:            time.Now,
	})
	proxy := httptest.NewServer(served.ForgeProxy())
	t.Cleanup(func() {
		proxy.Close()
		require.NoError(t, served.Close())
	})

	for _, engine := range []string{"harry", "ada"} {
		instance := komradtest.Start(t, komradtest.Params{ForgeProxy: config.ForgeProxy{
			URL:    "http://" + engine + ":" + engines[engine] + "@" + proxy.Listener.Addr().String(),
			CAFile: ca.CertFile,
		}})

		t.Run(engine+"'s server connects github.com as "+engine, func(t *testing.T) {
			instance.ConnectThroughForgeProxy()

			listed, err := instance.Forges.ListForges(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetForges(), 1)
			assert.Equal(t, "github.com", listed.Msg.GetForges()[0].GetHost())
			assert.Equal(t, "gho_"+engine, listed.Msg.GetForges()[0].GetLogin())
		})

		t.Run("gh in a shell on "+engine+"'s engine reaches GitHub as "+engine, func(t *testing.T) {
			if runtime.GOOS != "linux" {
				t.Skip("gh trusts the control plane's CA through SSL_CERT_FILE, which Go reads on Linux only")
			}
			_, err := exec.LookPath("gh")
			require.NoError(t, err)

			stream, err := openShell(t, instance, t.TempDir())
			require.NoError(t, err)
			require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("gh api user --jq .login; exit\n")}}))

			var output []byte
			for {
				response, err := stream.Receive()
				require.NoError(t, err)

				output = append(output, response.GetOutput()...)
				if response.GetExit() != nil {
					break
				}
			}

			assert.Contains(t, string(output), "gho_"+engine+"\r\n")
		})
	}
}
