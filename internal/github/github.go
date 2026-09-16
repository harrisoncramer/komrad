// Package github mints the short-lived token an engine fetches and pushes with.
//
// A machine that runs agents needs to reach a forge, and the obvious ways to let it are a
// personal token or a deploy key, both of which are long-lived secrets sitting on a shared box.
// A GitHub App is the alternative: the box holds the App's private key, signs a brief assertion
// with it, and exchanges that for an installation token which expires in an hour and is scoped to
// the repositories the App was installed on. Losing the box costs an hour rather than an account.
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// assertionLife is how far ahead of now the assertion's expiry is set, leaving the span it covers
// inside the ten minutes GitHub allows even with the minute its issued-at is backdated by.
const assertionLife = 8 * time.Minute

// renewWithin is how close to expiry a cached token is replaced rather than handed out, so a
// long push does not lose its credential partway through.
const renewWithin = 5 * time.Minute

// api is where installation tokens are minted, as a variable so a test can answer instead.
var api = "https://api.github.com"

// Token is an installation token and the moment it stops working.
type Token struct {
	Value     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Fresh reports whether a token is worth handing out rather than minting again.
func (t Token) Fresh(now time.Time) bool {
	return t.Value != "" && t.ExpiresAt.After(now.Add(renewWithin))
}

// App is the identity an engine reaches GitHub as.
type App struct {
	// ID is the App's own id, which identifies the key the assertion is signed with.
	ID string
	// InstallationID is the installation the token is minted for, which is what scopes it to
	// repositories rather than to everything the App could ever reach.
	InstallationID string
	// PrivateKeyPath is the PEM the App was created with.
	PrivateKeyPath string
}

// Configured reports whether an engine was told how to reach GitHub at all.
func (a App) Configured() bool {
	return a.ID != "" && a.InstallationID != "" && a.PrivateKeyPath != ""
}

// Mint exchanges the App's key for an installation token.
func (a App) Mint(ctx context.Context, client *http.Client, now time.Time) (Token, error) {
	if !a.Configured() {
		return Token{}, errors.New("no github app is configured, so there is nothing to mint a token from")
	}

	key, err := a.readKey()
	if err != nil {
		return Token{}, err
	}

	assertion, err := signAssertion(a.ID, key, now)
	if err != nil {
		return Token{}, err
	}

	url := fmt.Sprintf("%s/app/installations/%s/access_tokens", api, a.InstallationID)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
	if err != nil {
		return Token{}, fmt.Errorf("failed to ask for an installation token: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+assertion)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := client.Do(request)
	if err != nil {
		return Token{}, fmt.Errorf("failed to reach github for an installation token: %w", err)
	}

	defer func() { _ = response.Body.Close() }() //nolint:errcheck // the body is read out already

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Token{}, fmt.Errorf("failed to read github's answer: %w", err)
	}

	if response.StatusCode != http.StatusCreated {
		return Token{}, fmt.Errorf("github refused to mint an installation token for app %s installation %s: %s: %s",
			a.ID, a.InstallationID, response.Status, strings.TrimSpace(string(body)))
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return Token{}, fmt.Errorf("failed to read the token github returned: %w", err)
	}

	if token.Value == "" {
		return Token{}, errors.New("github returned no token, which means the app or the installation is not what it says")
	}

	return token, nil
}

func (a App) readKey() (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(a.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the github app key at %s: %w", a.PrivateKeyPath, err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%s is not a PEM key, so it is not the one the app was created with", a.PrivateKeyPath)
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the github app key at %s: %w", a.PrivateKeyPath, err)
	}

	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the key at %s is not RSA, which is the only kind github signs with", a.PrivateKeyPath)
	}

	return key, nil
}

func signAssertion(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(assertionLife).Unix(),
		"iss": appID,
	}

	encoded, err := joinSegments(header, claims)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(encoded))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign the github assertion: %w", err)
	}

	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func joinSegments(parts ...any) (string, error) {
	segments := make([]string, 0, len(parts))

	for _, part := range parts {
		encoded, err := json.Marshal(part)
		if err != nil {
			return "", fmt.Errorf("failed to build the github assertion: %w", err)
		}

		segments = append(segments, base64.RawURLEncoding.EncodeToString(encoded))
	}

	return strings.Join(segments, "."), nil
}

// Cache is where a minted token is kept so that every git operation does not mint its own.
type Cache struct {
	Path string
}

// Read returns the cached token, or a zero one when there is nothing worth reusing.
func (c Cache) Read() Token {
	if c.Path == "" {
		return Token{}
	}

	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return Token{}
	}

	var token Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return Token{}
	}

	return token
}

// Write keeps a token for the next caller, readable only by the account that minted it.
func (c Cache) Write(token Token) error {
	if c.Path == "" {
		return nil
	}

	encoded, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to record the github token: %w", err)
	}

	// Written to a file that is always new and renamed over the target, so the mode is the one
	// asked for rather than whatever the target was first created with, and a reader never sees
	// half a document.
	temporary, err := os.CreateTemp(filepath.Dir(c.Path), ".github-token-*")
	if err != nil {
		return fmt.Errorf("failed to record the github token at %s: %w", c.Path, err)
	}

	defer func() { _ = os.Remove(temporary.Name()) }() //nolint:errcheck // gone already on success

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close() //nolint:errcheck // the chmod is the failure worth reporting

		return fmt.Errorf("failed to record the github token at %s: %w", c.Path, err)
	}

	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close() //nolint:errcheck // the write is the failure worth reporting

		return fmt.Errorf("failed to record the github token at %s: %w", c.Path, err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to record the github token at %s: %w", c.Path, err)
	}

	if err := os.Rename(temporary.Name(), c.Path); err != nil {
		return fmt.Errorf("failed to record the github token at %s: %w", c.Path, err)
	}

	return nil
}

// CredentialReply is what a git credential helper writes back for a query it can answer.
func CredentialReply(token string) string {
	var out bytes.Buffer

	out.WriteString("username=x-access-token\n")
	out.WriteString("password=" + token + "\n")

	return out.String()
}

// CredentialQuery is the host git is asking about, read from the key-value block it writes.
func CredentialQuery(input string) map[string]string {
	query := map[string]string{}

	for _, line := range strings.Split(input, "\n") {
		name, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}

		query[name] = value
	}

	return query
}

// InstallationIDFromString is the installation an App was told to act as, refused when it is not
// a number, since GitHub names installations numerically and a name here is a copied placeholder.
func InstallationIDFromString(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
		return "", fmt.Errorf("%q is not an installation id, which github writes as a number", value)
	}

	return trimmed, nil
}
