package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"weave/config"
	"weave/internal/doctor"
	"weave/internal/engine"
	"weave/internal/ui/enginepick"
	"weave/internal/version"
	"weave/lib/xslice"
)

// errPickerClosed is the picker being closed with nothing chosen.
var errPickerClosed = errors.New("no engine was chosen")

// engineSetup is one engine's config as this machine reads it.
type engineSetup struct {
	env    *config.Environment
	remote engine.Remote
}

// loadEngine reads the config stack with the named engine laid over it, and this machine's own
// when the name is empty.
func loadEngine(stack config.Stack, name string) (*engineSetup, error) {
	cfg, err := config.LoadStack(config.Stack{Paths: stack.Paths, Engine: name})
	if err != nil {
		return nil, err
	}

	env, err := loadEnvironment(cfg)
	if err != nil {
		return nil, err
	}

	return &engineSetup{env: env, remote: remoteOf(env)}, nil
}

// engineRows is what the picker offers: this machine first, then every engine file.
func engineRows(stack config.Stack) ([]enginepick.Row, error) {
	names, err := config.EngineNames()
	if err != nil {
		return nil, err
	}

	rows := xslice.Map(names, func(name string) enginepick.Row {
		return describeEngine(stack, name)
	})

	return append([]enginepick.Row{describeEngine(stack, "")}, rows...), nil
}

func describeEngine(stack config.Stack, name string) enginepick.Row {
	row := enginepick.Row{Name: name}

	if name != "" {
		if path, err := config.EnginePath(name); err == nil {
			row.Facts = append(row.Facts, enginepick.Fact{Label: "file", Value: path})
		}
	}

	setup, err := loadEngine(stack, name)
	if err != nil {
		row.Facts = append(row.Facts, enginepick.Fact{Label: "config", Value: err.Error()})

		return row
	}

	row.Facts = append(row.Facts,
		enginepick.Fact{Label: "addr", Value: setup.env.Addr},
		enginepick.Fact{Label: "tmux", Value: setup.env.TmuxSocket},
	)

	if setup.remote.IsRemote() {
		row.Facts = append(row.Facts,
			enginepick.Fact{Label: "engine", Value: setup.remote.String()},
			enginepick.Fact{Label: "socket", Value: setup.remote.Socket},
		)
	}

	row.Facts = append(row.Facts, enginepick.Fact{Label: "cache", Value: setup.env.CacheDir})

	return row
}

type engineProber struct {
	stack config.Stack
}

func (p engineProber) probe(ctx context.Context, row enginepick.Row) enginepick.Status {
	setup, err := loadEngine(p.stack, row.Name)
	if err != nil {
		return enginepick.Status{Build: "config could not be read", Label: "unreadable", Refused: err.Error()}
	}

	addr := setup.env.Addr

	if setup.remote.IsRemote() {
		tunnel, err := setup.remote.Dial(ctx, addr, tunnelWait)
		if err != nil {
			return enginepick.Status{Build: "not reachable", Label: "down", Refused: err.Error()}
		}

		addr = tunnel.Local
	}

	served := probeServer(ctx, addr)

	switch {
	case !served.answering && setup.remote.IsRemote():
		return enginepick.Status{
			Build:   "not answering",
			Label:   "down",
			Refused: "the engine's server is not answering on " + addr,
		}
	case !served.answering:
		return enginepick.Status{Build: "nothing yet, the server starts on open"}
	case !served.current && setup.remote.IsRemote():
		return enginepick.Status{
			Build: version.Describe(served.version, served.commit),
			Label: "mismatch",
			Refused: fmt.Sprintf("the engine runs other code than this weave, %s: %s",
				version.Describe(version.Version(), version.Commit()),
				doctor.StaleEngineFix(served.version, served.commit)),
		}
	default:
		return enginepick.Status{Build: version.Describe(served.version, served.commit)}
	}
}

// pickEngine puts the picker up over the rows and hands back the engine chosen, empty for this
// machine.
func pickEngine(ctx context.Context, state *app, rows []enginepick.Row) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	prober := engineProber{stack: state.configStack}

	row, chosen, err := enginepick.Run(ctx, enginepick.RunParams{
		Rows: rows,
		Probe: func(row enginepick.Row) enginepick.Status {
			return prober.probe(ctx, row)
		},
		Self: version.Describe(version.Version(), version.Commit()),
	})
	if err != nil {
		return "", err
	}

	if !chosen {
		return "", errPickerClosed
	}

	return row.Name, nil
}

// openEngineArgs is the argv that runs this open again on the chosen engine.
func openEngineArgs(self string, stack config.Stack, name string) []string {
	args := []string{self, "open", "--engine", name}

	for _, path := range stack.Paths {
		args = append(args, "--config", path)
	}

	return args
}

// runOpenEngine runs an open on the chosen engine in this terminal and returns once it closes.
func runOpenEngine(ctx context.Context, self string, stack config.Stack, name string) error {
	args := openEngineArgs(self, stack, name)

	cmd := exec.CommandContext(ctx, self, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open the %s engine with %s: %w", name, strings.Join(args, " "), err)
	}

	return nil
}
