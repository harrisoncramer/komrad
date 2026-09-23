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

const (
	tenantVariable = "WEAVE_TENANT_ID"
	tenantFile     = "tenant_id"
)

// GetTenantID is the tenant this engine writes as, minted into the state directory the first time it is asked for.
func (e *Environment) GetTenantID() (string, error) {
	if id := os.Getenv(tenantVariable); id != "" {
		return parseTenantID(id, tenantVariable)
	}

	path := filepath.Join(e.StateDir, tenantFile)
	id, err := readTenantID(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return id, err
	}

	if err := mintTenantID(e.StateDir, path); err != nil {
		return "", err
	}

	return readTenantID(path)
}

func readTenantID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return parseTenantID(strings.TrimSpace(string(data)), path)
}

func parseTenantID(id, source string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("the tenant id in %s is %q, which is not a uuid: %w", source, id, err)
	}

	return id, nil
}

// mintTenantID links a fresh id into place unless another process already has.
func mintTenantID(dir, path string) (err error) {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to mint a tenant id: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, tenantFile+".*")
	if err != nil {
		return fmt.Errorf("failed to write a tenant id: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(tmp.Name())) }()

	if _, err := tmp.WriteString(id.String() + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write a tenant id: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write a tenant id: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write a tenant id: %w", err)
	}

	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("failed to save the tenant id to %s: %w", path, err)
	}

	return nil
}
