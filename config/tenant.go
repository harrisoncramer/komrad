package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const tenantFile = "tenant_id"

// ErrNoTenantID is what reading the tenant returns on an engine that has never started.
var ErrNoTenantID = errors.New("this engine has no tenant id yet; start it once with weave serve")

// GetTenantID reads the tenant this engine writes as.
func (e *Environment) GetTenantID() (string, error) {
	data, err := os.ReadFile(e.tenantPath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoTenantID
	}
	if err != nil {
		return "", fmt.Errorf("failed to read the tenant id: %w", err)
	}

	id := strings.TrimSpace(string(data))
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("the tenant id in %s is %q, which is not a uuid: %w", e.tenantPath(), id, err)
	}

	return id, nil
}

// EnsureTenantID reads the tenant this engine writes as, minting it on the engine's first start.
func (e *Environment) EnsureTenantID() (string, error) {
	id, err := e.GetTenantID()
	if !errors.Is(err, ErrNoTenantID) {
		return id, err
	}

	minted, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to mint a tenant id: %w", err)
	}

	if err := os.MkdirAll(e.StateDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", e.StateDir, err)
	}

	file, err := os.OpenFile(e.tenantPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("failed to save the tenant id: %w", err)
	}

	_, writeErr := file.WriteString(minted.String() + "\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return "", fmt.Errorf("failed to save the tenant id: %w", err)
	}

	return minted.String(), nil
}

func (e *Environment) tenantPath() string {
	return filepath.Join(e.StateDir, tenantFile)
}
