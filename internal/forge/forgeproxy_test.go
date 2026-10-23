package forge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/dbtest"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// viewerForge is a GitHub behind the forge proxy, naming whoever the engine's login belongs to.
type viewerForge struct {
	fakeForge
	viewer  Viewer
	viewErr error
}

func (f *viewerForge) ReadViewer(ctx context.Context) (*Viewer, error) {
	if _, err := f.auth.Credential(ctx); err != nil {
		return nil, err
	}
	if f.viewErr != nil {
		return nil, f.viewErr
	}

	return &f.viewer, nil
}

// TestConnectThroughForgeProxyKeepsGitHubsConnectionOnTheEnginesPerson covers the engine's
// github.com connection made, followed to a new account, and marked refused.
func TestConnectThroughForgeProxyKeepsGitHubsConnectionOnTheEnginesPerson(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		// connectedAs is who the connection already names, empty for an engine with none.
		connectedAs  string
		viewer       Viewer
		viewErr      error
		wantErr      error
		wantLogin    string
		wantVerified bool
	}{
		{
			name:         "an engine with no connection gets one holding the placeholder",
			viewer:       Viewer{Login: "harry", ID: 7},
			wantLogin:    "harry",
			wantVerified: true,
		},
		{
			name:         "a person who changed GitHub accounts moves the connection with them",
			connectedAs:  "harry-old",
			viewer:       Viewer{Login: "harry", ID: 7},
			wantLogin:    "harry",
			wantVerified: true,
		},
		{
			name:        "a forge proxy holding no login marks the connection refused",
			connectedAs: "harry",
			viewErr:     ErrUnauthorized,
			wantErr:     ErrUnauthorized,
			wantLogin:   "harry",
		},
		{
			name:    "a forge proxy holding no login leaves an engine with no connection without one",
			viewErr: ErrUnauthorized,
			wantErr: ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := store.Open(t.Context(), dbtest.URL(t))
			require.NoError(t, err)
			t.Cleanup(func() { _ = pool.Close() })

			connections := forgestore.NewForgeStore(pool.GetDB())
			credentials := NewFileStore(t.TempDir())
			login := tt.connectedAs

			service := NewService(NewServiceParams{
				Connections: connections,
				Credentials: credentials,
				Builders: map[forgev1.Kind]Builder{
					forgev1.Kind_KIND_GITHUB: func(_ string, auth Authenticator) Forge {
						return &viewerForge{fakeForge: fakeForge{auth: auth, login: login}, viewer: tt.viewer, viewErr: tt.viewErr}
					},
				},
				Now: func() time.Time { return now },
			})

			if tt.connectedAs != "" {
				_, err := service.Connect(t.Context(), ConnectParams{Host: "github.com", Token: NewSecret(config.ForgeProxyPlaceholder)})
				require.NoError(t, err)
			}
			login = tt.viewer.Login

			viewer, err := service.ConnectThroughForgeProxy(t.Context())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.viewer, *viewer)
			}

			connection, err := connections.GetByHost(t.Context(), "github.com")
			if tt.wantLogin == "" {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLogin, connection.Login)
			assert.Equal(t, tt.wantVerified, connection.VerifiedAt != nil)

			token, err := credentials.Get(t.Context(), connection.ID)
			require.NoError(t, err)
			assert.Equal(t, config.ForgeProxyPlaceholder, token.Expose())
		})
	}
}

// TestWriteCommitIdentity covers the file git reads the name and email from, whatever the
// person's GitHub profile holds.
func TestWriteCommitIdentity(t *testing.T) {
	tests := []struct {
		name        string
		viewer      Viewer
		wantContent string
	}{
		{
			name:        "a profile with a name and a public email",
			viewer:      Viewer{Login: "harry", Name: "Harry Cramer", Email: "harry@example.com", ID: 7},
			wantContent: "[user]\n\tname = \"Harry Cramer\"\n\temail = \"harry@example.com\"\n",
		},
		{
			name:        "a profile with no name and a private email",
			viewer:      Viewer{Login: "harry", ID: 7},
			wantContent: "[user]\n\tname = \"harry\"\n\temail = \"7+harry@users.noreply.github.com\"\n",
		},
		{
			name:        "a name git would otherwise read as syntax",
			viewer:      Viewer{Login: "harry", Name: `Harry "H" \ Cramer ; # x` + "\nemail = evil", Email: "harry@example.com", ID: 7},
			wantContent: "[user]\n\tname = \"Harry \\\"H\\\" \\\\ Cramer ; # x email = evil\"\n\temail = \"harry@example.com\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "git", "identity")
			require.NoError(t, WriteCommitIdentity(path, tt.viewer))

			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, string(content))
		})
	}
}

// TestWriteCommitIdentityLeavesAnUnchangedFileAlone covers the loop writing the same identity
// every minute without replacing the file git reads.
func TestWriteCommitIdentityLeavesAnUnchangedFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git", "identity")
	viewer := Viewer{Login: "harry", ID: 7}

	require.NoError(t, WriteCommitIdentity(path, viewer))
	first, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, WriteCommitIdentity(path, viewer))
	second, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, os.SameFile(first, second))

	require.NoError(t, WriteCommitIdentity(path, Viewer{Login: "harry", Name: "Harry", ID: 7}))
	third, err := os.Stat(path)
	require.NoError(t, err)
	assert.False(t, os.SameFile(first, third))
}
