//go:build integration

package e2e

import (
	"os"
	"testing"

	"weave/internal/weavetest"
	forgev1 "weave/proto/gen/forge/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// forgeTokenEnv names a GitHub token that reads harrisoncramer/weave-forge-fixture, and the
// live test skips without one so the suite never needs the network.
const forgeTokenEnv = "WEAVE_E2E_GITHUB_TOKEN"

// TestForgeConnectsThroughTheService covers the path a person takes to connect a forge, with
// the service doing every part of it: the token goes in once, the connection lists with the
// login GitHub answered, a check confirms it, a pull request is read through it, and
// disconnecting takes it away.
func TestForgeConnectsThroughTheService(t *testing.T) {
	token := os.Getenv(forgeTokenEnv)
	if token == "" {
		t.Skipf("%s is not set", forgeTokenEnv)
	}

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	connected, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: token}))
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", connected.Msg.GetForge().GetLogin())
	assert.True(t, connected.Msg.GetForge().GetHasCredential())

	_, err = instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: token}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	listed, err := instance.Forges.ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetForges(), 1)
	assert.NotContains(t, listed.Msg.String(), token, "the token never comes back")

	checked, err := instance.Forges.CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Len(t, checked.Msg.GetChecks(), 1)
	assert.Empty(t, checked.Msg.GetChecks()[0].GetProblem())

	pullRequests, err := instance.PullRequests.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/weave-forge-fixture.git",
		Branch:    "fixture/open",
	}))
	require.NoError(t, err)
	require.Len(t, pullRequests.Msg.GetPullRequests(), 1)
	assert.Equal(t, int32(1), pullRequests.Msg.GetPullRequests()[0].GetNumber())
	assert.Nil(t, pullRequests.Msg.GetPullRequests()[0].MergedAt)

	_, err = instance.PullRequests.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl: "git@gitlab.com:harrisoncramer/nothing.git",
		Branch:    "main",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "a host nothing is connected to")

	_, err = instance.Forges.DisconnectForge(ctx, connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))
	require.NoError(t, err)

	gone, err := instance.Forges.ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	assert.Empty(t, gone.Msg.GetForges())
}
