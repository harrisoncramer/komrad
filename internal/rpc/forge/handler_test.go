package forge

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
	"weave/lib/xptr"
	forgev1 "weave/proto/gen/forge/v1"
	forgev1weave "weave/proto/gen/forge/v1/forgev1weave"
	paginationv1 "weave/proto/gen/pagination/v1"

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
	checkErr     error
	held         bool
	connected    []forge.ConnectParams
	disconnected []string
	stamped      []*time.Time
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

// List answers connections unless err refuses it.
func (f *fakeConnector) List(context.Context) ([]forgestore.Connection, error) {
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

// Check answers checkErr.
func (f *fakeConnector) Check(context.Context, forgestore.Connection) (string, error) {
	return "", f.checkErr
}

// HasCredential answers held.
func (f *fakeConnector) HasCredential(context.Context, forgestore.Connection) (bool, error) {
	return f.held, nil
}

// SetVerifiedAt records the stamp.
func (f *fakeConnector) SetVerifiedAt(_ context.Context, _ string, at *time.Time) error {
	f.stamped = append(f.stamped, at)

	return nil
}

// ConnectionFor records the host and answers the first connection unless err refuses it.
func (f *fakeConnector) ConnectionFor(_ context.Context, host string) (*forgestore.Connection, error) {
	f.hosts = append(f.hosts, host)
	if f.err != nil {
		return nil, f.err
	}

	return &f.connections[0], nil
}

// ForConnection answers forge.
func (f *fakeConnector) ForConnection(forgestore.Connection) (forge.Forge, error) {
	return f.forge, nil
}

// TestForgeRefusalsMapToCodes pins the code each refusal reaches the caller as.
func TestForgeRefusalsMapToCodes(t *testing.T) {
	connectForge := func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error {
		_, err := service.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{
			Host:  "github.com",
			Token: "tok",
		}))

		return err
	}
	listForges := func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error {
		_, err := service.ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))

		return err
	}
	checkForges := func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error {
		_, err := service.CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))

		return err
	}
	disconnectForge := func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error {
		_, err := service.DisconnectForge(ctx, connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))

		return err
	}
	listPullRequests := func(remote, cursor string) func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error {
		return func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error {
			req := &forgev1.ListForgePullRequestsRequest{RemoteUrl: remote}
			if cursor != "" {
				req.Pagination = &paginationv1.Pagination{Cursor: xptr.Ptr(cursor)}
			}

			_, err := service.ListForgePullRequests(ctx, connect.NewRequest(req))

			return err
		}
	}
	remote := "git@github.com:harrisoncramer/weave.git"

	tests := []struct {
		name     string
		call     func(ctx context.Context, service forgev1weave.ForgeServiceAdapter) error
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
				connections: []forgestore.Connection{connected()},
				forge:       &fakeForge{},
				err:         tt.err,
			}

			err := tt.call(t.Context(), forgev1weave.ForgeServiceAdapter{Impl: NewHandler(connector)})
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestConnectForge pins that the token reaches the service and never comes back.
func TestConnectForge(t *testing.T) {
	connector := &fakeConnector{
		connections: []forgestore.Connection{connected()},
		held:        true,
	}
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(connector)}

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
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(connector)}

	_, err := service.DisconnectForge(t.Context(), connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))
	require.NoError(t, err)

	assert.Equal(t, []string{"github.com"}, connector.disconnected)
}

// TestCheckForges pins which answers of a check clear the connection's stamp.
func TestCheckForges(t *testing.T) {
	tests := []struct {
		name         string
		checkErr     error
		wantKind     string
		wantStamped  bool
		wantVerified bool
	}{
		{
			name:         "a refusal clears the stamp",
			checkErr:     forge.ErrUnauthorized.Saying("Bad credentials"),
			wantKind:     ProblemRefused,
			wantStamped:  true,
			wantVerified: false,
		},
		{
			name:         "a missing credential clears the stamp",
			checkErr:     forge.ErrNoCredential.Saying("no file"),
			wantKind:     ProblemNoCredential,
			wantStamped:  true,
			wantVerified: false,
		},
		{
			name:         "an unreachable forge leaves the stamp alone",
			checkErr:     errors.New("dial tcp: no such host"),
			wantKind:     ProblemUnreachable,
			wantStamped:  false,
			wantVerified: true,
		},
		{
			name:         "an accepted credential restores the stamp",
			wantStamped:  true,
			wantVerified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &fakeConnector{
				connections: []forgestore.Connection{connected()},
				checkErr:    tt.checkErr,
			}
			service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(connector)}

			resp, err := service.CheckForges(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			require.Len(t, resp.Msg.GetChecks(), 1)

			check := resp.Msg.GetChecks()[0]
			assert.Equal(t, tt.wantKind, check.GetProblemKind())
			assert.Equal(t, tt.wantStamped, len(connector.stamped) == 1)
			assert.Equal(t, tt.wantVerified, check.GetForge().VerifiedAt != nil)
		})
	}
}

func connected() forgestore.Connection {
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
