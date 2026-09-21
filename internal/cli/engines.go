package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"

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
			row.Facts = append(row.Facts, enginepick.Fact{Label: "file   ", Value: path})
		}
	}

	setup, err := loadEngine(stack, name)
	if err != nil {
		row.Facts = append(row.Facts, enginepick.Fact{Label: "config ", Value: err.Error()})

		return row
	}

	row.Facts = append(row.Facts,
		enginepick.Fact{Label: "addr   ", Value: setup.env.Addr},
		enginepick.Fact{Label: "tmux   ", Value: setup.env.TmuxSocket},
	)

	if setup.remote.IsRemote() {
		row.Facts = append(row.Facts,
			enginepick.Fact{Label: "engine ", Value: setup.remote.String()},
			enginepick.Fact{Label: "socket ", Value: setup.remote.Socket},
		)
	}

	row.Facts = append(row.Facts, enginepick.Fact{Label: "cache  ", Value: setup.env.CacheDir})

	return row
}

// engineProber probes each row once and keeps the tunnels it dialled, so the one chosen is
// still up for the open that follows and the rest can be closed.
type engineProber struct {
	stack   config.Stack
	mu      sync.Mutex
	tunnels map[string]*engine.Tunnel
}

func newEngineProber(stack config.Stack) *engineProber {
	return &engineProber{stack: stack, tunnels: map[string]*engine.Tunnel{}}
}

// probe is the picker's Probe for one row.
func (p *engineProber) probe(ctx context.Context, row enginepick.Row) enginepick.Status {
	setup, err := loadEngine(p.stack, row.Name)
	if err != nil {
		return enginepick.Status{Build: "config could not be read", Refused: err.Error()}
	}

	addr := setup.env.Addr

	if setup.remote.IsRemote() {
		tunnel, err := setup.remote.Dial(ctx, addr, tunnelWait)
		if err != nil {
			return enginepick.Status{Build: "not reachable", Refused: err.Error()}
		}

		p.mu.Lock()
		p.tunnels[row.Name] = tunnel
		p.mu.Unlock()

		addr = tunnel.Local
	}

	served := probeServer(ctx, addr)

	switch {
	case !served.answering && setup.remote.IsRemote():
		return enginepick.Status{Build: "not answering", Refused: "the engine's server is not answering on " + addr}
	case !served.answering:
		return enginepick.Status{Build: "nothing yet, the server starts on open"}
	case !served.current && setup.remote.IsRemote():
		return enginepick.Status{
			Build: version.Describe(served.version, served.commit),
			Refused: fmt.Sprintf("the engine runs other code than this weave, %s: %s",
				version.Describe(version.Version(), version.Commit()),
				doctor.StaleEngineFix(served.version, served.commit)),
		}
	default:
		return enginepick.Status{Build: version.Describe(served.version, served.commit)}
	}
}

// closeAllBut closes every tunnel the probes dialled except the chosen engine's, which the
// open that follows borrows.
func (p *engineProber) closeAllBut(chosen string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, tunnel := range p.tunnels {
		if name != chosen {
			_ = tunnel.Close()
		}
	}
}

// pickEngine puts the picker up when there is more than this machine to choose from, and hands
// back the engine chosen, empty for this machine.
func pickEngine(ctx context.Context, state *app) (string, error) {
	rows, err := engineRows(state.configStack)
	if err != nil {
		return "", err
	}

	if len(rows) == 1 {
		return "", nil
	}

	prober := newEngineProber(state.configStack)

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
		prober.closeAllBut("")

		return "", errPickerClosed
	}

	prober.closeAllBut(row.Name)

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

// execOpenEngine replaces this process with an open on the chosen engine.
func execOpenEngine(self string, stack config.Stack, name string) error {
	args := openEngineArgs(self, stack, name)

	if err := syscall.Exec(self, args, os.Environ()); err != nil {
		return fmt.Errorf("failed to open the %s engine with %s: %w", name, strings.Join(args, " "), err)
	}

	return nil
}
