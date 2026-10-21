//go:build integration

package e2e

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/controlplane"
	"komrad/internal/controlplanetest"
	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/interceptors"
	"komrad/internal/komradtest"
	"komrad/internal/oidctest"
	"komrad/internal/store"
	"komrad/lib/xhttp"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signedTransport sends each request with the person's ID token, the way a panel signed in to
// the engine does.
type signedTransport struct {
	token string
	next  http.RoundTripper
}

func (s signedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	signed := r.Clone(r.Context())
	signed.Header.Set("Authorization", "Bearer "+s.token)

	return s.next.RoundTrip(signed)
}

// TestIntegrationControlPlaneSendsEachEnginesPathToTheEngine covers a person signed in to an
// engine reaching it through the control plane at the engine's path, by a call and by a shell.
func TestIntegrationControlPlaneSendsEachEnginesPathToTheEngine(t *testing.T) {
	provider := oidctest.Start(t)
	signIn := config.SignIn{Issuer: provider.URL, ClientID: oidctest.ClientID}
	instance := komradtest.Start(t, komradtest.Params{SignIn: signIn})
	instance.Stores.SeedAccess(t, oidctest.Email)

	public, err := url.Parse(instance.PublicURL)
	require.NoError(t, err)

	grants, err := store.OpenGrantStore(t.Context(), dbtest.New(t).OwnerURL)
	require.NoError(t, err)
	t.Cleanup(grants.Close)

	ca := controlplanetest.NewCA(t)
	github := controlplanetest.StartGitHub(t, ca)
	served := controlplane.New(controlplane.NewParams{
		Logger:         slog.New(slog.DiscardHandler),
		Engines:        map[string]config.ControlPlaneEngine{"harry": {Credential: "credential-harry", Addr: public.Host}},
		SignIn:         signIn,
		CA:             ca.Cert,
		Logins:         forge.NewFileStore(t.TempDir()),
		Grants:         grants,
		Verifier:       interceptors.NewProviderVerifier(interceptors.NewProviderVerifierParams{Issuer: provider.URL, ClientID: oidctest.ClientID}),
		GitHubClientID: "Iv1.komrad",
		GitHub:         github.URL,
		GitHubAPI:      github.URL,
		Client:         github.Client(),
		Upstream:       github.Upstream(),
		Engine:         controlplane.NewEngineTransport(),
		Now:            time.Now,
	})
	endpoints := httptest.NewUnstartedServer(served.Handler())
	endpoints.Config.Protocols = xhttp.NewServerProtocols()
	endpoints.Start()
	t.Cleanup(func() {
		endpoints.Close()
		require.NoError(t, served.Close())
	})

	engineURL := endpoints.URL + "/harry"
	signed := xhttp.NewClientOver(signedTransport{token: provider.Sign(provider.Claims(nil)), next: xhttp.NewStreamTransport()}, 0)

	t.Run("calls and a shell reach the engine", func(t *testing.T) {
		workflow, err := workflowv1connect.NewWorkflowServiceClient(signed, engineURL).CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
			Name: "backend",
			Cwd:  t.TempDir(),
		}))
		require.NoError(t, err)

		created, err := agentv1connect.NewAgentServiceClient(signed, engineURL).CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "fix the flaky login test",
			Title:      "login",
			WorkflowId: workflow.Msg.GetWorkflow().GetId(),
		}))
		require.NoError(t, err)

		stream := terminalv1connect.NewTerminalServiceClient(signed, engineURL).Shell(t.Context())
		t.Cleanup(func() { _ = stream.CloseResponse() })

		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Open{Open: &terminalv1.OpenShell{
			Dir:  created.Msg.GetAgent().GetCwd(),
			Size: &terminalv1.Size{Rows: 24, Cols: 80},
		}}}))
		ready, err := stream.Receive()
		require.NoError(t, err)
		require.NotNil(t, ready.GetReady())

		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("echo through-the-plane; exit\n")}}))

		var output []byte
		for {
			response, err := stream.Receive()
			require.NoError(t, err)

			output = append(output, response.GetOutput()...)
			if response.GetExit() != nil {
				break
			}
		}

		assert.Contains(t, string(output), "through-the-plane\r\n")
	})
}
