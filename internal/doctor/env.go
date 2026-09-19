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
	CheckForges           func(ctx context.Context) ([]ForgeStatus, error)
	ProbeServer           func(ctx context.Context) (ServerStatus, error)
	IsPanelRunning        func(ctx context.Context) bool
	ReadTerminfo          func(ctx context.Context, term, capability string) (string, error)

	// MeasureColumns asks the terminal how wide it drew each sample, and is left nil wherever
	// there is no terminal to take over.
	MeasureColumns func(ctx context.Context, samples []string) ([]int, error)
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
	EngineIsRemote      bool
	Version             string
	Commit              string
	Term                string
	ColorTerm           string
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
		ReadTerminfo:          readTerminfo,
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
