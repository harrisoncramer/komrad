package forge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"komrad/internal/fail"
	"komrad/lib/xfile"

	"github.com/google/uuid"
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

// credentialStore keeps each connection's token somewhere only komrad's user can read.
type credentialStore interface {
	// Get is the token for a connection, or ErrNoCredential.
	Get(ctx context.Context, connectionID uuid.UUID) (Secret, error)
	Set(ctx context.Context, connectionID uuid.UUID, secret Secret) error
	// Delete takes a connection's token away, and is content when there was none.
	Delete(ctx context.Context, connectionID uuid.UUID) error
}

// credentialDir is where FileStore keeps its files under the state directory.
const credentialDir = "forge"

// FileStore keeps each token in a file of its own, mode 0600.
type FileStore struct {
	stateDir string
}

// NewFileStore builds a store under the state directory.
func NewFileStore(stateDir string) *FileStore {
	return &FileStore{stateDir: stateDir}
}

// Path is the file holding a connection's token.
func (f *FileStore) Path(connectionID uuid.UUID) string {
	return filepath.Join(f.stateDir, credentialDir, connectionID.String())
}

// Get is the token on disk for a connection, or ErrNoCredential.
func (f *FileStore) Get(_ context.Context, connectionID uuid.UUID) (Secret, error) {
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

// Set puts a token on disk where only komrad's user can read it.
func (f *FileStore) Set(_ context.Context, connectionID uuid.UUID, secret Secret) error {
	path := f.Path(connectionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to make the credential directory: %w", err)
	}

	if err := xfile.WriteAtomic(path, []byte(secret.Expose()+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write the credential for connection %s: %w", connectionID, err)
	}

	return nil
}

// Delete takes a connection's token off disk.
func (f *FileStore) Delete(_ context.Context, connectionID uuid.UUID) error {
	err := os.Remove(f.Path(connectionID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove the credential for connection %s: %w", connectionID, err)
	}

	return nil
}

// verifiedSetter is the one write an Authenticator makes back to its connection.
type verifiedSetter interface {
	SetVerifiedAt(ctx context.Context, id uuid.UUID, at *time.Time) error
}

// noRecord is a verifiedSetter that writes nothing, for a caller that only reports.
type noRecord struct{}

// SetVerifiedAt does nothing.
func (noRecord) SetVerifiedAt(context.Context, uuid.UUID, *time.Time) error {
	return nil
}

// storedCredential is a token a person supplied, read from the store on each use.
type storedCredential struct {
	store        credentialStore
	connectionID uuid.UUID
	header       string
	connections  verifiedSetter
}

// newStoredCredentialParams says whose token to read and how to send it.
type newStoredCredentialParams struct {
	Store        credentialStore
	ConnectionID uuid.UUID
	Header       string
	Connections  verifiedSetter
}

// newStoredCredential builds an Authenticator over the store.
func newStoredCredential(params newStoredCredentialParams) *storedCredential {
	return &storedCredential{
		store:        params.Store,
		connectionID: params.ConnectionID,
		header:       params.Header,
		connections:  params.Connections,
	}
}

// Credential is the token in the store.
func (s *storedCredential) Credential(ctx context.Context) (Credential, error) {
	token, err := s.store.Get(ctx, s.connectionID)
	if err != nil {
		return Credential{}, err
	}

	return Credential{Header: s.header, Token: token}, nil
}

// Invalidate clears the connection's verified_at.
func (s *storedCredential) Invalidate(ctx context.Context) error {
	return s.connections.SetVerifiedAt(ctx, s.connectionID, nil)
}
