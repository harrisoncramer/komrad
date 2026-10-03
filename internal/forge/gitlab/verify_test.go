package gitlab

import (
	"net/http"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerify covers asking GitLab who the credential is, a token GitLab refuses reaching the
// Authenticator, and a token that reaches nobody.
func TestVerify(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Verify": {body: `{"data":{"currentUser":{"username":"harrisoncramer"}}}`}})

	login, err := f.Verify(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
	assert.Equal(t, verifyQuery, fake.last(t).Query)

	refused, auth, _ := serve(t, map[string]answer{"Verify": {status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`}})
	_, err = refused.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)

	nobody, _, _ := serve(t, map[string]answer{"Verify": {body: `{"data":{"currentUser":null}}`}})
	_, err = nobody.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
}
