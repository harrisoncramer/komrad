package gitlab

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/forge"
)

const selfTokenPath = "/personal_access_tokens/self"

// TestReadToken covers the scopes and expiry GitLab reports, and the answers that are not a token.
func TestReadToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answer  answer
		want    *forge.TokenInfo
		wantErr error
	}{
		{
			name:   "a token that expires",
			answer: answer{body: `{"id":42,"name":"komrad","revoked":false,"active":true,"scopes":["api","write_repository"],"user_id":7,"expires_at":"2026-10-04"}`},
			want: &forge.TokenInfo{
				Scopes:    []string{"api", "write_repository"},
				ExpiresAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name:   "a token that never expires",
			answer: answer{body: `{"id":42,"scopes":["read_api"],"expires_at":null}`},
			want:   &forge.TokenInfo{Scopes: []string{"read_api"}},
		},
		{
			name:    "a token gitlab refuses",
			answer:  answer{status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:    "an instance with no self endpoint",
			answer:  answer{status: http.StatusNotFound, body: `{"message":"404 Not Found"}`},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, map[string]answer{selfTokenPath: tt.answer})

			got, err := f.ReadToken(t.Context())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, http.MethodGet, fake.last(t).Method)
		})
	}
}

// TestRotateToken covers the new token GitLab hands back, and the rotations it refuses.
func TestRotateToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		answer          answer
		want            string
		wantErr         error
		wantInvalidated int
	}{
		{
			name:   "gitlab hands back the new token",
			answer: answer{body: `{"id":43,"name":"komrad","revoked":false,"active":true,"scopes":["api"],"expires_at":"2026-09-22","token":"glpat-new"}`},
			want:   "glpat-new",
		},
		{
			name:            "a token already rotated away revokes the family",
			answer:          answer{status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`},
			wantErr:         forge.ErrUnauthorized,
			wantInvalidated: 1,
		},
		{
			name:   "a token without the scope to rotate",
			answer: answer{status: http.StatusForbidden, body: `{"message":"403 Forbidden"}`},
		},
		{
			name:   "an answer with no token in it",
			answer: answer{body: `{"id":43,"scopes":["api"]}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, auth, fake := serve(t, map[string]answer{selfTokenPath + "/rotate": tt.answer})

			got, err := f.RotateToken(t.Context(), time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC))
			assert.Equal(t, tt.wantInvalidated, auth.invalidated)
			assert.Equal(t, http.MethodPost, fake.last(t).Method)
			assert.Equal(t, map[string]any{"expires_at": "2026-09-22"}, fake.last(t).Body)
			if tt.want == "" {
				require.Error(t, err)
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				}

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Expose())
		})
	}
}

// TestRevokeToken covers a revoke GitLab accepts and one it refuses.
func TestRevokeToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answer  answer
		wantErr error
	}{
		{
			name:   "gitlab revokes the token",
			answer: answer{status: http.StatusNoContent},
		},
		{
			name:    "a token gitlab already refuses",
			answer:  answer{status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`},
			wantErr: forge.ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, map[string]answer{selfTokenPath: tt.answer})

			err := f.RevokeToken(t.Context())
			assert.Equal(t, http.MethodDelete, fake.last(t).Method)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
		})
	}
}
