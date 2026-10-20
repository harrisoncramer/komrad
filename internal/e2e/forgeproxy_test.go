//go:build integration

package e2e

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	agentv1 "komrad/proto/gen/agent/v1"
	terminalv1 "komrad/proto/gen/terminal/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

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

	engines := map[string]config.ControlPlaneEngine{
		"harry": {Credential: "credential-harry", Addr: "127.0.0.1:1"},
		"ada":   {Credential: "credential-ada", Addr: "127.0.0.1:1"},
	}
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

	// komrad stands in for the engine's binary, which gh's stand-in never runs on an engine with a forge proxy.
	komrad := filepath.Join(t.TempDir(), "komrad")
	require.NoError(t, os.WriteFile(komrad, []byte("#!/bin/sh\nexit 1\n"), 0o700))

	for _, engine := range []string{"harry", "ada"} {
		instance := komradtest.Start(t, komradtest.Params{
			ForgeProxy: config.ForgeProxy{
				URL:    "http://" + engine + ":" + engines[engine].Credential + "@" + proxy.Listener.Addr().String(),
				CAFile: ca.CertFile,
			},
			HookCommand: komrad,
		})

		t.Run(engine+"'s server connects github.com as "+engine, func(t *testing.T) {
			instance.ConnectThroughForgeProxy()

			listed, err := instance.Forges.ListForges(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetForges(), 1)
			assert.Equal(t, "github.com", listed.Msg.GetForges()[0].GetHost())
			assert.Equal(t, "gho_"+engine, listed.Msg.GetForges()[0].GetLogin())
		})

		t.Run("gh in a shell on "+engine+"'s engine reaches GitHub as "+engine, func(t *testing.T) {
			_, err := exec.LookPath("gh")
			require.NoError(t, err)

			workflow, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
				Name: "backend",
				Cwd:  t.TempDir(),
			}))
			require.NoError(t, err)

			created, err := instance.Agents.CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{
				Prompt:     "fix the flaky login test",
				Title:      "login",
				WorkflowId: workflow.Msg.GetWorkflow().GetId(),
			}))
			require.NoError(t, err)

			stream, err := openShell(t, instance, created.Msg.GetAgent().GetCwd())
			require.NoError(t, err)
			require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("unset GH_TOKEN GITHUB_TOKEN; gh api user --jq .login | cat; exit\n")}}))

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
