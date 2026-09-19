package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	app := Identity{
		ID:             "12345",
		InstallationID: "678",
		PrivateKeyPath: writeKey(t),
	}
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

// TestMintExplainsARefusal covers which of GitHub's answers to a mint count as a refusal.
func TestMintExplainsARefusal(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		wantRefused bool
	}{
		{name: "a key github cannot check", status: http.StatusUnauthorized, wantRefused: true},
		{name: "a suspended installation", status: http.StatusForbidden, wantRefused: true},
		{name: "an installation github does not have", status: http.StatusNotFound, wantRefused: true},
		{name: "a rate limit", status: http.StatusTooManyRequests, wantRefused: false},
		{name: "github failing to answer", status: http.StatusBadGateway, wantRefused: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"` + http.StatusText(tt.status) + `"}`))
			}))
			defer server.Close()

			app := Identity{
				ID:             "12345",
				InstallationID: "678",
				PrivateKeyPath: writeKey(t),
				API:            server.URL,
			}
			_, err := app.Mint(t.Context(), server.Client(), time.Now())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "12345")
			assert.Contains(t, err.Error(), "678")
			assert.Equal(t, tt.wantRefused, errors.Is(err, ErrRefused))
		})
	}
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
	cache := Cache{Path: filepath.Join(t.TempDir(), "token.json"), Identity: Identity{
		ID:             "12345",
		InstallationID: "678",
		PrivateKeyPath: writeKey(t),
	}}
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
	cache := Cache{Path: filepath.Join(t.TempDir(), "token.json"), Identity: Identity{
		ID:             "12345",
		InstallationID: "678",
		PrivateKeyPath: writeKey(t),
	}}
	require.NoError(t, os.WriteFile(cache.Path, []byte("{}"), 0o644))

	require.NoError(t, cache.Write(Token{Value: "ghs_abc", ExpiresAt: time.Now().Add(time.Hour)}))

	info, err := os.Stat(cache.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, "ghs_abc", cache.Read().Value)
}

// TestCacheIgnoresATokenMintedForAnotherApp covers the App the config names being changed under a
// cache that is still fresh.
func TestCacheIgnoresATokenMintedForAnotherApp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	key := writeKey(t)
	minted := Cache{Path: path, Identity: Identity{
		ID:             "12345",
		InstallationID: "678",
		PrivateKeyPath: key,
	}}
	require.NoError(t, minted.Write(Token{Value: "ghs_abc", ExpiresAt: time.Now().Add(time.Hour)}))

	assert.Equal(t, "ghs_abc", minted.Read().Value)

	for name, identity := range map[string]Identity{
		"another app": {
			ID:             "99999",
			InstallationID: "678",
			PrivateKeyPath: key,
		},
		"another installation": {
			ID:             "12345",
			InstallationID: "999999",
			PrivateKeyPath: key,
		},
		"another key": {
			ID:             "12345",
			InstallationID: "678",
			PrivateKeyPath: writeKey(t),
		},
		"no key at all": {
			ID:             "12345",
			InstallationID: "678",
			PrivateKeyPath: filepath.Join(t.TempDir(), "gone.pem"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, Cache{Path: path, Identity: identity}.Read().Value)
		})
	}
}

// TestFetchMintsForAnAppTheCacheDoesNotName covers the check an operator runs after changing the
// config.
func TestFetchMintsForAnAppTheCacheDoesNotName(t *testing.T) {
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Token{Value: "ghs_new", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	defer server.Close()

	old := api
	api = server.URL
	defer func() { api = old }()

	path := filepath.Join(t.TempDir(), "token.json")
	stale := Cache{Path: path, Identity: Identity{
		ID:             "12345",
		InstallationID: "678",
		PrivateKeyPath: writeKey(t),
	}}
	require.NoError(t, stale.Write(Token{Value: "ghs_old", ExpiresAt: time.Now().Add(time.Hour)}))

	cache := Cache{Path: path, Identity: Identity{
		ID:             "99999",
		InstallationID: "999999",
		PrivateKeyPath: writeKey(t),
	}}

	token, err := cache.Fetch(t.Context(), server.Client(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "ghs_new", token.Value)
	assert.Equal(t, 1, asked)
	assert.Equal(t, "ghs_new", cache.Read().Value)
}

// TestFetchKeepsATokenItCannotCache covers a full disk or a cache directory that cannot be
// written into.
func TestFetchKeepsATokenItCannotCache(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a directory whatever its mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Token{Value: "ghs_abc", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	defer server.Close()

	old := api
	api = server.URL
	defer func() { api = old }()

	dir := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.Mkdir(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	cache := Cache{
		Path: filepath.Join(dir, "token.json"),
		Identity: Identity{
			ID:             "12345",
			InstallationID: "678",
			PrivateKeyPath: writeKey(t),
		},
	}

	token, err := cache.Fetch(t.Context(), server.Client(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "ghs_abc", token.Value)
}

// TestSlugAsksAsTheApp covers the name read under the App's own assertion rather than an
// installation token, and github refusing to describe an app that is not what it says.
func TestSlugAsksAsTheApp(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		if r.URL.Path != "/app" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		_, _ = w.Write([]byte(`{"slug":"weave-engine"}`))
	}))
	defer server.Close()

	old := api
	api = server.URL
	defer func() { api = old }()

	app := Identity{
		ID:             "12345",
		InstallationID: "678",
		PrivateKeyPath: writeKey(t),
	}
	slug, err := app.Slug(t.Context(), server.Client(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "weave-engine", slug)
	assert.True(t, strings.HasPrefix(seen, "Bearer "))
	assert.Len(t, strings.Split(strings.TrimPrefix(seen, "Bearer "), "."), 3, "the app speaks with its assertion, not a token")

	_, err = Identity{}.Slug(t.Context(), server.Client(), time.Now())
	require.Error(t, err)
}

// TestForgetDropsTheCachedToken covers a stale token being thrown away so the next fetch mints,
// and forgetting when there is nothing to forget.
func TestForgetDropsTheCachedToken(t *testing.T) {
	cache := Cache{Path: filepath.Join(t.TempDir(), "token"), Identity: Identity{
		ID:             "1",
		InstallationID: "2",
		PrivateKeyPath: writeKey(t),
	}}
	require.NoError(t, cache.Write(Token{Value: "ghs_old", ExpiresAt: time.Now().Add(time.Hour)}))
	require.Equal(t, "ghs_old", cache.Read().Value)

	require.NoError(t, cache.Forget())
	assert.Empty(t, cache.Read().Value)
	require.NoError(t, cache.Forget())
	require.NoError(t, Cache{}.Forget())
}
