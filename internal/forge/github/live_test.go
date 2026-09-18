//go:build integration

package github

import (
	"context"
	"os"
	"testing"

	"weave/internal/forge"
	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TokenEnv names the token the live test runs with, and the test skips without it.
const TokenEnv = "WEAVE_FORGE_TEST_GITHUB_TOKEN"

type envAuth struct{ token string }

func (e envAuth) Credential(context.Context) (forge.Credential, error) {
	return forge.Credential{Header: forge.HeaderBearer, Token: e.token}, nil
}

func (e envAuth) Invalidate(context.Context) error { return nil }

// TestLiveFixture reads the private fixture repository harrisoncramer/weave-forge-fixture,
// which holds one open pull request on fixture/open and one merged on fixture/merged, and pins
// that the provider sees both as GitHub has them.
func TestLiveFixture(t *testing.T) {
	token := os.Getenv(TokenEnv)
	if token == "" {
		t.Skipf("%s is not set", TokenEnv)
	}

	f := New(NewParams{Auth: envAuth{token: token}})
	ctx := t.Context()

	login, err := f.Verify(ctx)
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)

	fixture, err := git.ParseRemoteURL("git@github.com:harrisoncramer/weave-forge-fixture.git")
	require.NoError(t, err)

	open, err := f.ListOnBranch(ctx, fixture, "fixture/open")
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, 1, open[0].Number)
	assert.Equal(t, "fixture: open", open[0].Title)
	assert.Equal(t, "https://github.com/harrisoncramer/weave-forge-fixture", open[0].Repository.Canonical())
	assert.False(t, open[0].IsFinished())
	assert.False(t, open[0].IsCrossRepository())

	merged, err := f.ListOnBranch(ctx, fixture, "fixture/merged")
	require.NoError(t, err)
	require.Len(t, merged, 1)
	assert.Equal(t, 2, merged[0].Number)
	assert.NotNil(t, merged[0].MergedAt)
	assert.NotNil(t, merged[0].ClosedAt)
	assert.True(t, merged[0].IsFinished())

	byURL, err := f.Get(ctx, "https://github.com/harrisoncramer/weave-forge-fixture/pull/1")
	require.NoError(t, err)
	assert.Equal(t, open[0].ProviderID, byURL.ProviderID)

	none, err := f.ListOnBranch(ctx, fixture, "no-such-branch")
	require.NoError(t, err)
	assert.Empty(t, none)
}
