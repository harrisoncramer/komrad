// Package forge is the boundary between komrade and the service a repository's pull requests
// live on.
package forge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"komrade/internal/fail"
)

// ErrNoCredential says a connection has no credential in the store.
var ErrNoCredential = fail.Precondition("the connection holds no credential")

// HeaderBearer is the header a GitHub token and a GitLab OAuth token are sent in.
const HeaderBearer = "Authorization"

// HeaderPrivateToken is the header a GitLab personal token is sent in.
const HeaderPrivateToken = "PRIVATE-TOKEN"

// Credential is what a forge is shown on each request.
type Credential struct {
	// Header is which header carries the token, HeaderBearer or HeaderPrivateToken.
	Header string
	Token  Secret
	// ExpiresAt is when the token stops working, zero for one that does not expire.
	ExpiresAt time.Time
}

// HeaderValue is what goes after the header name on the wire.
func (c Credential) HeaderValue() string {
	if c.Header == HeaderBearer {
		return "Bearer " + c.Token.Expose()
	}

	return c.Token.Expose()
}

// Authenticator is where a connection's credential comes from.
type Authenticator interface {
	Credential(ctx context.Context) (Credential, error)
	// Invalidate says the forge refused the credential.
	Invalidate(ctx context.Context) error
}

// CredentialStore keeps each connection's token somewhere only komrade's user can read.
type CredentialStore interface {
	// Get is the token for a connection, or ErrNoCredential.
	Get(ctx context.Context, connectionID string) (Secret, error)
	Set(ctx context.Context, connectionID string, secret Secret) error
	// Delete takes a connection's token away, and is content when there was none.
	Delete(ctx context.Context, connectionID string) error
}

// CredentialDir is where FileStore keeps its files under the state directory.
const CredentialDir = "forge"

// FileStore keeps each token in a file of its own, mode 0600.
type FileStore struct {
	stateDir string
}

// NewFileStore builds a store under the state directory.
func NewFileStore(stateDir string) *FileStore {
	return &FileStore{stateDir: stateDir}
}

// Path is the file holding a connection's token.
func (f *FileStore) Path(connectionID string) string {
	return filepath.Join(f.stateDir, CredentialDir, connectionID)
}

// Get is the token on disk for a connection, or ErrNoCredential.
func (f *FileStore) Get(_ context.Context, connectionID string) (Secret, error) {
	raw, err := os.ReadFile(f.Path(connectionID))
	if errors.Is(err, fs.ErrNotExist) {
		return Secret{}, ErrNoCredential.Saying("no credential file for connection %s", connectionID)
	}
	if err != nil {
		return Secret{}, fmt.Errorf("failed to read the credential for connection %s: %w", connectionID, err)
	}

	secret := NewSecret(string(raw))
	if secret.IsZero() {
		return Secret{}, ErrNoCredential.Saying("the credential file for connection %s is empty", connectionID)
	}

	return secret, nil
}

// Set puts a token on disk where only komrade's user can read it.
func (f *FileStore) Set(_ context.Context, connectionID string, secret Secret) error {
	path := f.Path(connectionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to make the credential directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(secret.Expose()+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write the credential for connection %s: %w", connectionID, err)
	}

	return os.Chmod(path, 0o600)
}

// Delete takes a connection's token off disk.
func (f *FileStore) Delete(_ context.Context, connectionID string) error {
	err := os.Remove(f.Path(connectionID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove the credential for connection %s: %w", connectionID, err)
	}

	return nil
}

// verifiedSetter is the one write an Authenticator makes back to its connection.
type verifiedSetter interface {
	SetVerifiedAt(ctx context.Context, id string, at *time.Time) error
}

// NoRecord is a verifiedSetter that writes nothing, for a caller that only reports.
type NoRecord struct{}

// SetVerifiedAt does nothing.
func (NoRecord) SetVerifiedAt(context.Context, string, *time.Time) error {
	return nil
}

// NewStoredCredentialParams says whose token to read and how to send it.
type NewStoredCredentialParams struct {
	Store        CredentialStore
	ConnectionID string
	Header       string
	Connections  verifiedSetter
}

// StoredCredential is a token a person supplied, read from the store on each use.
type StoredCredential struct {
	store        CredentialStore
	connectionID string
	header       string
	connections  verifiedSetter
}

// NewStoredCredential builds an Authenticator over the store.
func NewStoredCredential(params NewStoredCredentialParams) *StoredCredential {
	return &StoredCredential{
		store:        params.Store,
		connectionID: params.ConnectionID,
		header:       params.Header,
		connections:  params.Connections,
	}
}

// Credential is the token in the store.
func (s *StoredCredential) Credential(ctx context.Context) (Credential, error) {
	token, err := s.store.Get(ctx, s.connectionID)
	if err != nil {
		return Credential{}, err
	}

	return Credential{Header: s.header, Token: token}, nil
}

// Invalidate clears the connection's verified_at.
func (s *StoredCredential) Invalidate(ctx context.Context) error {
	return s.connections.SetVerifiedAt(ctx, s.connectionID, nil)
}
