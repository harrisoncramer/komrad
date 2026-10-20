package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"komrad/internal/fail"

	"github.com/zalando/go-keyring"
)

// keyringService is the keychain service every engine's sign-in is kept under, one entry per url.
const keyringService = "komrad"

// SignInPath is where `komrad login` reads which provider an engine trusts, before it has a token.
const SignInPath = "/sign-in"

var (
	ErrSignInNeeded        = fail.Precondition("sign in to the engine with komrad login")
	ErrAccessDenied        = fail.Denied("nobody has granted this account access to the engine, so ask its owner")
	ErrProviderUnreachable = fail.Precondition("failed to reach the identity provider")
	ErrEngineStopped       = fail.Precondition("the engine is stopped")
	ErrEngineStarting      = fail.Precondition("the engine is starting")
)

var errNotSignedIn = ErrSignInNeeded.Saying("this machine has not signed in to the engine, so run komrad login")

// SignInProvider is the provider an engine trusts, and komrad's id and secret there.
type SignInProvider struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// signIn is what this machine keeps for one engine a person signed in to.
type signIn struct {
	Provider     SignInProvider `json:"provider"`
	Email        string         `json:"email"`
	Subject      string         `json:"subject"`
	IDToken      string         `json:"id_token"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	Expiry       time.Time      `json:"expiry"`
}

// NewSignInKeeper keeps sign-ins in the keychain, or in private files under dir where the keychain
// is missing or refuses one.
func NewSignInKeeper(dir string) *SignInKeeper {
	return &SignInKeeper{dir: dir}
}

// SignInKeeper reads and writes the sign-in this machine holds for each engine url.
type SignInKeeper struct {
	dir string
}

// GetSignIn reads the sign-in this machine holds for the engine at url.
func (k *SignInKeeper) GetSignIn(url string) (*signIn, error) {
	encoded, err := keyring.Get(keyringService, url)
	if err != nil {
		encoded, err = k.readFile(url)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNotSignedIn.Saying("this machine has not signed in to %s, so run komrad login", url)
	}
	if err != nil {
		return nil, err
	}

	var kept signIn
	if err := json.Unmarshal([]byte(encoded), &kept); err != nil {
		return nil, fmt.Errorf("failed to read the sign-in for %s: %w", url, err)
	}

	return &kept, nil
}

// SaveSignIn keeps the sign-in for the engine at url, in place of any it held before.
func (k *SignInKeeper) SaveSignIn(url string, kept signIn) error {
	encoded, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("failed to encode the sign-in for %s: %w", url, err)
	}

	if err := keyring.Set(keyringService, url, string(encoded)); err == nil {
		_ = os.Remove(k.getPath(url)) //nolint:errcheck // the keychain is read first, so a file left behind is never read

		return nil
	}

	_ = keyring.Delete(keyringService, url) //nolint:errcheck // a keychain that refused the write may hold no entry to delete

	return k.writeFile(url, encoded)
}

// DeleteSignIn drops the sign-in this machine holds for the engine at url, from the keychain and
// from the file, and succeeds when it held none.
func (k *SignInKeeper) DeleteSignIn(url string) error {
	if _, err := keyring.Get(keyringService, url); err == nil {
		if err := keyring.Delete(keyringService, url); err != nil {
			return fmt.Errorf("failed to drop the sign-in for %s from the keychain: %w", url, err)
		}
	}

	if err := os.Remove(k.getPath(url)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to drop the sign-in for %s: %w", url, err)
	}

	return nil
}

func (k *SignInKeeper) readFile(url string) (string, error) {
	encoded, err := os.ReadFile(k.getPath(url))
	if err != nil {
		return "", fmt.Errorf("failed to read the sign-in for %s: %w", url, err)
	}

	return string(encoded), nil
}

func (k *SignInKeeper) writeFile(url string, encoded []byte) error {
	path := k.getPath(url)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to make the directory for the sign-in to %s: %w", url, err)
	}

	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("failed to write the sign-in for %s: %w", url, err)
	}

	return nil
}

func (k *SignInKeeper) getPath(url string) string {
	sum := sha256.Sum256([]byte(url))

	return filepath.Join(k.dir, "sign-in", hex.EncodeToString(sum[:])+".json")
}
