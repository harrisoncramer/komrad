package forge

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"
	forgev1komrad "komrad/proto/gen/forge/v1/forgev1komrad"
	paginationv1 "komrad/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeConnector answers from its fields and records what each call asked of it.
type fakeConnector struct {
	connections  []forgestore.Connection
	forge        forge.Forge
	err          error
	checked      forge.CheckResult
	held         bool
	connected    []forge.ConnectParams
	disconnected []string
	hosts        []string
}

// Connect records the params and answers the first connection unless err refuses it.
func (f *fakeConnector) Connect(_ context.Context, params forge.ConnectParams) (*forgestore.Connection, error) {
	f.connected = append(f.connected, params)
	if f.err != nil {
		return nil, f.err
	}

	return &f.connections[0], nil
}

// ListConnections answers connections unless err refuses it.
func (f *fakeConnector) ListConnections(context.Context) ([]forgestore.Connection, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.connections, nil
}

// Disconnect records the host and answers err.
func (f *fakeConnector) Disconnect(_ context.Context, host string) error {
	f.disconnected = append(f.disconnected, host)

	return f.err
}

// RecheckConnection answers checked unless err refuses it.
func (f *fakeConnector) RecheckConnection(context.Context, forgestore.Connection) (*forge.CheckResult, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.checked, nil
}

// HasCredential answers held.
func (f *fakeConnector) HasCredential(context.Context, forgestore.Connection) (bool, error) {
	return f.held, nil
}

// ForHost records the host and answers forge unless err refuses it.
func (f *fakeConnector) ForHost(_ context.Context, host string) (forge.Forge, error) {
	f.hosts = append(f.hosts, host)
	if f.err != nil {
		return nil, f.err
	}

	return f.forge, nil
}

// TestForgeRefusalsMapToCodes pins the code each refusal reaches the caller as.
func TestForgeRefusalsMapToCodes(t *testing.T) {
	connectForge := func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error {
		_, err := service.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{
			Host:  "github.com",
			Token: "tok",
		}))

		return err
	}
	listForges := func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error {
		_, err := service.ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))

		return err
	}
	checkForges := func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error {
		_, err := service.CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))

		return err
	}
	disconnectForge := func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error {
		_, err := service.DisconnectForge(ctx, connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))

		return err
	}
	listPullRequests := func(remote, cursor string) func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error {
		return func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error {
			req := &forgev1.ListForgePullRequestsRequest{RemoteUrl: remote}
			if cursor != "" {
				req.Pagination = &paginationv1.Pagination{Cursor: xptr.Ptr(cursor)}
			}

			_, err := service.ListForgePullRequests(ctx, connect.NewRequest(req))

			return err
		}
	}
	remote := "git@github.com:harrisoncramer/komrad.git"

	tests := []struct {
		name     string
		call     func(ctx context.Context, service forgev1komrad.ForgeServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name:     "connecting a host already connected",
			call:     connectForge,
			err:      forgestore.ErrHostTaken.Saying("github.com is already connected"),
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "listing forges the store cannot read",
			call:     listForges,
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "checking forges the store cannot read",
			call:     checkForges,
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "disconnecting a host never connected",
			call:     disconnectForge,
			err:      store.ErrNotFound.Saying("github.com is not connected"),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "listing pull requests of a remote that is not a forge",
			call:     listPullRequests("/local/path", ""),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "listing pull requests on a host nothing is connected to",
			call:     listPullRequests(remote, ""),
			err:      store.ErrNotFound.Saying("github.com is not connected"),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a page token that is not one",
			call:     listPullRequests(remote, "not-a-token"),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a page token with no cursor",
			call:     listPullRequests(remote, base64.URLEncoding.EncodeToString([]byte(`{"per_page":20}`))),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a page token with no page size",
			call:     listPullRequests(remote, base64.URLEncoding.EncodeToString([]byte(`{"cursor":"Y3Vyc29yOjM="}`))),
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &fakeConnector{
				connections: []forgestore.Connection{newConnection()},
				forge:       &fakeForge{},
				err:         tt.err,
			}

			err := tt.call(t.Context(), forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(connector, nil, &fakeRemotes{})})
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestConnectForge pins that the token reaches the service and never comes back.
func TestConnectForge(t *testing.T) {
	connector := &fakeConnector{
		connections: []forgestore.Connection{newConnection()},
		held:        true,
	}
	service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(connector, nil, &fakeRemotes{})}

	resp, err := service.ConnectForge(t.Context(), connect.NewRequest(&forgev1.ConnectForgeRequest{
		Host:  "github.com",
		Token: "ghp_secret",
	}))
	require.NoError(t, err)

	require.Len(t, connector.connected, 1)
	assert.Equal(t, "ghp_secret", connector.connected[0].Token.Expose())
	assert.Equal(t, "harrisoncramer", resp.Msg.GetForge().GetLogin())
	assert.True(t, resp.Msg.GetForge().GetHasCredential())
	assert.NotContains(t, resp.Msg.String(), "ghp_")
}

// TestDisconnectForge pins that the host asked for reaches the service.
func TestDisconnectForge(t *testing.T) {
	connector := &fakeConnector{}
	service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(connector, nil, &fakeRemotes{})}

	_, err := service.DisconnectForge(t.Context(), connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))
	require.NoError(t, err)

	assert.Equal(t, []string{"github.com"}, connector.disconnected)
}

// TestCheckForges pins how each check's answer renders.
func TestCheckForges(t *testing.T) {
	refused := newConnection()
	refused.VerifiedAt = nil

	tests := []struct {
		name         string
		checked      forge.CheckResult
		wantProblem  string
		wantKind     string
		wantVerified bool
	}{
		{
			name: "a refusal carries its problem and kind",
			checked: forge.CheckResult{
				Connection:  refused,
				Problem:     forge.ErrUnauthorized.Saying("Bad credentials"),
				ProblemKind: forge.ProblemRefused,
			},
			wantProblem: "Bad credentials",
			wantKind:    forge.ProblemRefused,
		},
		{
			name:         "an accepted credential carries no problem",
			checked:      forge.CheckResult{Connection: newConnection()},
			wantVerified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &fakeConnector{
				connections: []forgestore.Connection{newConnection()},
				checked:     tt.checked,
			}
			service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(connector, nil, &fakeRemotes{})}

			resp, err := service.CheckForges(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			require.Len(t, resp.Msg.GetChecks(), 1)

			check := resp.Msg.GetChecks()[0]
			assert.Equal(t, tt.wantProblem, check.GetProblem())
			assert.Equal(t, tt.wantKind, check.GetProblemKind())
			assert.Equal(t, tt.wantVerified, check.GetForge().VerifiedAt != nil)
		})
	}
}

func newConnection() forgestore.Connection {
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	return forgestore.Connection{
		ID:         "conn-1",
		Kind:       forge.KindGitHub,
		Host:       "github.com",
		Method:     forge.MethodToken,
		Login:      "harrisoncramer",
		VerifiedAt: &at,
		CreatedAt:  at,
	}
}

// fakeRemotes parses a remote as git does, answering host in place of the remote's own when one is set.
type fakeRemotes struct {
	host string
}

func (f *fakeRemotes) ParseRemoteURL(_ context.Context, raw string) (git.RemoteURL, error) {
	remote, err := git.ParseRemoteURL(raw)
	if err != nil {
		return git.RemoteURL{}, err
	}
	if f.host != "" {
		remote.Host = f.host
	}

	return remote, nil
}
