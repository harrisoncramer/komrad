package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"weave/config"
)

const commandTimeout = 5 * time.Second

type Env struct {
	Settings
	LookPath              func(name string) (string, error)
	Output                func(ctx context.Context, name string, args ...string) (string, error)
	LoadConfig            func() (*config.Config, error)
	DoesLayerExist        func(path string) bool
	InspectDir            func(path string) (DirState, error)
	ReadFile              func(path string) ([]byte, error)
	PingDatabase          func(ctx context.Context, url string) error
	ReadPendingMigrations func(ctx context.Context, url string) ([]string, error)
	ConnectReadOnly       func(ctx context.Context, url string) error
	ReadWorktreeOwners    func(ctx context.Context, url string) (WorktreeOwners, error)
	ProbeServer           func(ctx context.Context, addr string) ServerStatus
	IsPanelRunning        func(ctx context.Context) bool
}

type HarnessInfo struct {
	ID     string
	Binary string
}

type Settings struct {
	TmuxSocket          string
	InsideTmux          string
	ConfigStack         config.Stack
	configLayers        []config.Layer
	Sources             map[string]config.Source
	Harnesses           []HarnessInfo
	DatabaseURL         string
	ReadOnlyDatabaseURL string
	Addr                string
	Version             string
}

// NewEnv is the env pointed at the machine this is running on.
func NewEnv(settings Settings) Env {
	settings.configLayers, _ = config.ResolveLayers(settings.ConfigStack) //nolint:errcheck // a stack that will not resolve is the config check's to report

	return Env{
		Settings:   settings,
		LookPath:   exec.LookPath,
		Output:     runOutput,
		LoadConfig: func() (*config.Config, error) { return config.LoadStack(settings.ConfigStack) },
		DoesLayerExist: func(path string) bool {
			_, err := os.Stat(path)

			return err == nil
		},
		InspectDir:            inspectDir,
		ReadFile:              os.ReadFile,
		PingDatabase:          pingDatabase,
		ReadPendingMigrations: readPendingMigrations,
		ConnectReadOnly:       connectReadOnly,
		ReadWorktreeOwners:    readWorktreeOwners,
	}
}

func runOutput(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}

		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}
