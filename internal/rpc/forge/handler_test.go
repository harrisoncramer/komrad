package forge

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
	forgev1 "weave/proto/gen/forge/v1"
	forgev1weave "weave/proto/gen/forge/v1/forgev1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeForges answers as a forge service with one connection, remembering what it was told.
type fakeForges struct {
	connection *forgestore.Connection
	checkErr   error
	held       bool
	connected  forge.ConnectParams
	stamped    []*time.Time
}

func (f *fakeForges) Connect(_ context.Context, params forge.ConnectParams) (*forgestore.Connection, error) {
	f.connected = params
	if f.connection == nil {
		return nil, forgestore.ErrHostTaken.Saying("%s is already connected", params.Host)
	}

	return f.connection, nil
}

func (f *fakeForges) List(context.Context) ([]forgestore.Connection, error) {
	if f.connection == nil {
		return nil, nil
	}

	return []forgestore.Connection{*f.connection}, nil
}

func (f *fakeForges) Disconnect(_ context.Context, host string) error {
	if f.connection == nil || f.connection.Host != host {
		return store.ErrNotFound.Saying("%s is not connected", host)
	}

	f.connection = nil

	return nil
}

func (f *fakeForges) Check(context.Context, forgestore.Connection) (string, error) {
	if f.checkErr != nil {
		return "", f.checkErr
	}

	return f.connection.Login, nil
}

func (f *fakeForges) HasCredential(context.Context, forgestore.Connection) (bool, error) {
	return f.held, nil
}

func (f *fakeForges) SetVerifiedAt(_ context.Context, _ string, at *time.Time) error {
	f.stamped = append(f.stamped, at)

	return nil
}

func connected() *forgestore.Connection {
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	return &forgestore.Connection{
		ID:         "conn-1",
		Kind:       forge.KindGitHub,
		Host:       "github.com",
		Method:     forge.MethodToken,
		Login:      "harrisoncramer",
		VerifiedAt: &at,
		CreatedAt:  at,
	}
}

// TestConnectForge covers the token travelling once into the service and the connection
// coming back without it.
func TestConnectForge(t *testing.T) {
	t.Parallel()

	forges := &fakeForges{connection: connected(), held: true}
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(forges)}

	resp, err := service.ConnectForge(t.Context(), connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: "ghp_secret"}))
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", forges.connected.Token.Expose())
	assert.Equal(t, "harrisoncramer", resp.Msg.GetForge().GetLogin())
	assert.True(t, resp.Msg.GetForge().GetHasCredential())
	assert.NotContains(t, resp.Msg.String(), "ghp_", "the token is never in what comes back")
}

// TestConnectForgeRefusal covers the service's refusal reaching the caller with its kind.
func TestConnectForgeRefusal(t *testing.T) {
	t.Parallel()

	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(&fakeForges{})}

	_, err := service.ConnectForge(t.Context(), connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: "tok"}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
}

// TestCheckForges covers the answer being recorded on the row: a stamp on success, and the
// stamp cleared on a refusal but not on a forge that could not be reached.
func TestCheckForges(t *testing.T) {
	t.Parallel()

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
			checkErr:     nil,
			wantKind:     "",
			wantStamped:  true,
			wantVerified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forges := &fakeForges{
				connection: connected(),
				held:       true,
				checkErr:   tt.checkErr,
			}
			service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(forges)}

			resp, err := service.CheckForges(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)
			require.Len(t, resp.Msg.GetChecks(), 1)

			check := resp.Msg.GetChecks()[0]
			assert.Equal(t, tt.wantKind, check.GetProblemKind())
			assert.Equal(t, tt.wantStamped, len(forges.stamped) == 1)
			assert.Equal(t, tt.wantVerified, check.GetForge().VerifiedAt != nil)
		})
	}
}

// TestDisconnectForge covers a host going, and one that was never there.
func TestDisconnectForge(t *testing.T) {
	t.Parallel()

	forges := &fakeForges{connection: connected()}
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(forges)}

	_, err := service.DisconnectForge(t.Context(), connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))
	require.NoError(t, err)

	_, err = service.DisconnectForge(t.Context(), connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: "github.com"}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}
