// Package forge is the boundary between weave and the service a repository's pull requests
// live on.
package forge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/internal/fail"
)

// ErrNoCredential says a connection has no credential on disk.
var ErrNoCredential = fail.Precondition("the connection holds no credential")

// HeaderBearer is the header a GitHub token and a GitLab OAuth token are sent in.
const HeaderBearer = "Authorization"

// HeaderPrivateToken is the header a GitLab personal token is sent in.
const HeaderPrivateToken = "PRIVATE-TOKEN"

// Credential is what a forge is shown on each request.
type Credential struct {
	// Header is which header carries the token, HeaderBearer or HeaderPrivateToken.
	Header string
	Token  string
	// ExpiresAt is when the token stops working, zero for one that does not expire.
	ExpiresAt time.Time
}

// HeaderValue is what goes after the header name on the wire.
func (c Credential) HeaderValue() string {
	if c.Header == HeaderBearer {
		return "Bearer " + c.Token
	}

	return c.Token
}

// Authenticator is where a connection's credential comes from.
type Authenticator interface {
	Credential(ctx context.Context) (Credential, error)
	// Invalidate says the forge refused the credential, so it is not to be trusted again.
	Invalidate(ctx context.Context) error
}

// CredentialDir is where credential files live under the state directory.
const CredentialDir = "forge"

// CredentialPath is the file holding a connection's token.
func CredentialPath(stateDir, connectionID string) string {
	return filepath.Join(stateDir, CredentialDir, connectionID)
}

// WriteCredential puts a token on disk where only weave's user can read it.
func WriteCredential(stateDir, connectionID, token string) error {
	path := CredentialPath(stateDir, connectionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to make the credential directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write the credential for connection %s: %w", connectionID, err)
	}

	return os.Chmod(path, 0o600)
}

// ReadCredential is the token on disk for a connection, or ErrNoCredential.
func ReadCredential(stateDir, connectionID string) (string, error) {
	raw, err := os.ReadFile(CredentialPath(stateDir, connectionID))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoCredential.Saying("no credential file for connection %s", connectionID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to read the credential for connection %s: %w", connectionID, err)
	}

	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", ErrNoCredential.Saying("the credential file for connection %s is empty", connectionID)
	}

	return token, nil
}

// RemoveCredential takes a connection's token off disk, and is content when there was none.
func RemoveCredential(stateDir, connectionID string) error {
	err := os.Remove(CredentialPath(stateDir, connectionID))
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
func (NoRecord) SetVerifiedAt(context.Context, string, *time.Time) error { return nil }

// FileCredential is a token a person supplied, read from its file on each use.
type FileCredential struct {
	stateDir     string
	connectionID string
	header       string
	connections  verifiedSetter
}

// NewFileCredentialParams says whose token to read and how to send it.
type NewFileCredentialParams struct {
	StateDir     string
	ConnectionID string
	Header       string
	Connections  verifiedSetter
}

// NewFileCredential builds an Authenticator over a credential file.
func NewFileCredential(params NewFileCredentialParams) *FileCredential {
	return &FileCredential{
		stateDir:     params.StateDir,
		connectionID: params.ConnectionID,
		header:       params.Header,
		connections:  params.Connections,
	}
}

// Credential is the token on disk.
func (f *FileCredential) Credential(context.Context) (Credential, error) {
	token, err := ReadCredential(f.stateDir, f.connectionID)
	if err != nil {
		return Credential{}, err
	}

	return Credential{Header: f.header, Token: token}, nil
}

// Invalidate clears the connection's verified_at so the row says it needs remaking.
func (f *FileCredential) Invalidate(ctx context.Context) error {
	return f.connections.SetVerifiedAt(ctx, f.connectionID, nil)
}
