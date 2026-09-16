package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "app.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600))
	return path
}

// TestMintSignsAnAssertionGitHubCanCheck covers the assertion carrying the app as its issuer and
// an expiry inside the window github allows, which is what it refuses a token over.
func TestMintSignsAnAssertionGitHubCanCheck(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Token{Value: "ghs_abc", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	defer server.Close()

	old := api
	api = server.URL
	defer func() { api = old }()

	now := time.Now()
	app := App{ID: "12345", InstallationID: "678", PrivateKeyPath: writeKey(t)}
	token, err := app.Mint(t.Context(), server.Client(), now)
	require.NoError(t, err)
	assert.Equal(t, "ghs_abc", token.Value)

	require.True(t, strings.HasPrefix(seen, "Bearer "))
	parts := strings.Split(strings.TrimPrefix(seen, "Bearer "), ".")
	require.Len(t, parts, 3)

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(raw, &claims))

	assert.Equal(t, "12345", claims["iss"])
	assert.Less(t, claims["exp"].(float64)-claims["iat"].(float64), float64(600),
		"github refuses an assertion whose own span is more than ten minutes")
	assert.Less(t, claims["iat"].(float64), float64(now.Unix()),
		"iat is backdated, so this host's clock running slow does not make the assertion future-dated")
}

// TestMintExplainsARefusal covers the App id or the installation being wrong, which is the whole
// failure surface of this setup and is otherwise a bare 404.
func TestMintExplainsARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	old := api
	api = server.URL
	defer func() { api = old }()

	app := App{ID: "12345", InstallationID: "678", PrivateKeyPath: writeKey(t)}
	_, err := app.Mint(t.Context(), server.Client(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "12345")
	assert.Contains(t, err.Error(), "678")
}

// TestFreshRefusesATokenAboutToExpire covers a long push losing its credential partway through.
func TestFreshRefusesATokenAboutToExpire(t *testing.T) {
	now := time.Now()
	assert.True(t, Token{Value: "x", ExpiresAt: now.Add(time.Hour)}.Fresh(now))
	assert.False(t, Token{Value: "x", ExpiresAt: now.Add(time.Minute)}.Fresh(now))
	assert.False(t, Token{ExpiresAt: now.Add(time.Hour)}.Fresh(now))
}

// TestCredentialQueryReadsGitsBlock covers the key-value block git writes on stdin.
func TestCredentialQueryReadsGitsBlock(t *testing.T) {
	query := CredentialQuery("protocol=https\nhost=github.com\n\n")
	assert.Equal(t, "github.com", query["host"])
	assert.Equal(t, "https", query["protocol"])
	assert.Contains(t, CredentialReply("ghs_abc"), "password=ghs_abc")
}

// TestInstallationIDRefusesAPlaceholder covers the id copied from the guide rather than the URL.
func TestInstallationIDRefusesAPlaceholder(t *testing.T) {
	_, err := InstallationIDFromString("INSTALLATION_ID")
	require.Error(t, err)

	value, err := InstallationIDFromString(" 678 ")
	require.NoError(t, err)
	assert.Equal(t, "678", value)
}

// TestCacheRoundTrips covers the token being reused rather than minted per git operation.
func TestCacheRoundTrips(t *testing.T) {
	cache := Cache{Path: filepath.Join(t.TempDir(), "token.json")}
	assert.Empty(t, cache.Read().Value)

	want := Token{Value: "ghs_abc", ExpiresAt: time.Now().Add(time.Hour).Truncate(time.Second)}
	require.NoError(t, cache.Write(want))
	assert.Equal(t, want.Value, cache.Read().Value)

	info, err := os.Stat(cache.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a token is readable only by who minted it")
}

// TestCacheTightensAFileSomebodyElseCreated covers the mode being the one asked for rather than
// whatever the path was first created with, which is what open(2) would otherwise leave alone.
func TestCacheTightensAFileSomebodyElseCreated(t *testing.T) {
	cache := Cache{Path: filepath.Join(t.TempDir(), "token.json")}
	require.NoError(t, os.WriteFile(cache.Path, []byte("{}"), 0o644))

	require.NoError(t, cache.Write(Token{Value: "ghs_abc", ExpiresAt: time.Now().Add(time.Hour)}))

	info, err := os.Stat(cache.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, "ghs_abc", cache.Read().Value)
}
