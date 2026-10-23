package shells

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"komrad/internal/agentdirs"
	"komrad/internal/harness"
	"komrad/internal/logging"
	"komrad/internal/pty"
)

// NewShellServiceParams is what the service is built over.
type NewShellServiceParams struct {
	Agents agentdirs.DirLister
	Shell  string
	// Tools are the gh and glab stand-ins put ahead of the shell's PATH, as an agent's are.
	Tools harness.WriteToolShimsParams
}

// ShellService opens shells in the directories agents work in.
type ShellService struct {
	agents agentdirs.DirLister
	shell  string
	tools  harness.WriteToolShimsParams
}

// NewShellService builds the service.
func NewShellService(params NewShellServiceParams) *ShellService {
	return &ShellService{agents: params.Agents, shell: params.Shell, tools: params.Tools}
}

// OpenParams is the shell a person asked for.
type OpenParams struct {
	Dir  string
	Size pty.Size
	Term string
}

// Open starts a shell in dir, which must be inside a live agent's directory, killed when ctx ends.
func (s *ShellService) Open(ctx context.Context, params OpenParams) (pty.Terminal, error) {
	dir, err := agentdirs.Resolve(ctx, s.agents, params.Dir)
	if err != nil {
		return nil, err
	}

	terminal, err := pty.Start(ctx, pty.StartParams{
		Argv: []string{s.shell},
		Dir:  dir,
		Env:  s.environment(ctx),
		Size: params.Size,
		Term: params.Term,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a shell in %s: %w", dir, err)
	}

	return terminal, nil
}

// environment is the engine's environment with the stand-ins ahead of its PATH, or nil for the
// engine's own when there are none.
func (s *ShellService) environment(ctx context.Context) []string {
	tools, err := harness.WriteToolShims(s.tools)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "gh and glab could not be given stand-ins, so this shell reaches its forge as the machine is logged in",
			logging.Err(err)...)

		return nil
	}
	if tools == "" {
		return nil
	}

	env := os.Environ()
	for i, variable := range env {
		if path, ok := strings.CutPrefix(variable, "PATH="); ok {
			env[i] = "PATH=" + tools + ":" + path

			return env
		}
	}

	return append(env, "PATH="+tools)
}
