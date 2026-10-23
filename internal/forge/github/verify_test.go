package github

import (
	"net/http"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerify covers asking GitHub who the credential is, and a token GitHub refuses.
func TestVerify(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Verify": {body: `{
	"data": {
		"viewer": {
			"login": "harrisoncramer"
		}
	}
}`}})

	login, err := f.Verify(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
	seen := fake.last(t)
	assert.Equal(t, verifyQuery, seen.Query)
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))

	refused, auth, _ := serve(t, map[string]answer{"Verify": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})
	_, err = refused.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestReadViewer covers the name, public email and account id a person commits as, read over
// REST, and a token GitHub refuses.
func TestReadViewer(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"/user": {body: `{"login":"harrisoncramer","name":"Harry Cramer","email":"harry@example.com","id":7}`}})

	viewer, err := f.ReadViewer(t.Context())
	require.NoError(t, err)
	assert.Equal(t, forge.Viewer{Login: "harrisoncramer", Name: "Harry Cramer", Email: "harry@example.com", ID: 7}, *viewer)
	assert.Equal(t, "/user", fake.last(t).Path)

	refused, auth, _ := serve(t, map[string]answer{"/user": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})
	_, err = refused.ReadViewer(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestVerifyInstallation covers how many repositories an installation token reaches, and a
// refusal reaching the Authenticator.
func TestVerifyInstallation(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"/installation/repositories": {body: `{"total_count":4,"repositories":[{"full_name":"harrisoncramer/komrad"}]}`}})

	reached, err := f.VerifyInstallation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, reached)
	assert.Equal(t, "/installation/repositories", fake.last(t).Path)

	refused, auth, _ := serve(t, map[string]answer{"/installation/repositories": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})
	_, err = refused.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)

	missing, _, _ := serve(t, nil)
	_, err = missing.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrNotFound)
}
