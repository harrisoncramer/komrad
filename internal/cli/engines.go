package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/config"
	"komrad/db"
	"komrad/internal/doctor"
	"komrad/internal/engine"
	"komrad/internal/store"
	"komrad/internal/ui"
	"komrad/internal/version"
	"komrad/lib/xslice"
)

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

// engineRows is every engine this machine can open, itself first.
func engineRows(stack config.Stack) ([]ui.EngineRow, error) {
	names, err := config.EngineNames()
	if err != nil {
		return nil, err
	}

	rows := xslice.Map(names, func(name string) ui.EngineRow {
		return describeEngine(stack, name)
	})

	return append([]ui.EngineRow{describeEngine(stack, "")}, rows...), nil
}

func describeEngine(stack config.Stack, name string) ui.EngineRow {
	row := ui.EngineRow{Name: name}

	if name != "" {
		if path, err := config.EnginePath(name); err == nil {
			row.Facts = append(row.Facts, ui.EngineFact{Label: "file", Value: path})
		}
	}

	setup, err := loadEngine(stack, name)
	if err != nil {
		row.Facts = append(row.Facts, ui.EngineFact{Label: "config", Value: err.Error()})

		return row
	}

	row.Facts = append(row.Facts,
		ui.EngineFact{Label: "addr", Value: setup.env.Addr},
		ui.EngineFact{Label: "tmux", Value: setup.env.TmuxSocket},
	)

	if setup.remote.IsRemote() {
		row.Facts = append(row.Facts, ui.EngineFact{Label: "engine", Value: setup.remote.String()})
	}

	row.Facts = append(row.Facts, ui.EngineFact{Label: "cache", Value: setup.env.CacheDir})

	return row
}

// loginWait bounds the fresh database login the chooser makes for an engine on this machine.
const loginWait = 3 * time.Second

type engineProber struct {
	stack      config.Stack
	checkLogin func(ctx context.Context, databaseURL string) error
}

// checkEngineLogin opens the store with a fresh login, as the engine's server does when it starts.
func checkEngineLogin(ctx context.Context, databaseURL string) error {
	ctx, cancel := context.WithTimeout(ctx, loginWait)
	defer cancel()

	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}

	return pool.Close()
}

func (p engineProber) probe(ctx context.Context, row ui.EngineRow) ui.EngineStatus {
	setup, err := loadEngine(p.stack, row.Name)
	if err != nil {
		return ui.EngineStatus{
			Build:   "config could not be read",
			Label:   "unreadable",
			Refused: err.Error(),
		}
	}

	var loginErr error
	if !setup.remote.IsRemote() {
		loginErr = p.checkLogin(ctx, setup.env.DatabaseURL)
	}

	addr := setup.env.Addr

	if setup.remote.IsRemote() {
		tunnel, err := setup.remote.Dial(ctx, addr, tunnelWait)
		if err != nil {
			return ui.EngineStatus{
				Build:   "not reachable",
				Label:   "down",
				Refused: err.Error(),
			}
		}

		addr = tunnel.Local
	}

	served := probeServer(ctx, addr)

	switch {
	case !served.answering && setup.remote.IsRemote():
		return ui.EngineStatus{
			Build:   "not answering",
			Label:   "down",
			Refused: "the engine's server is not answering on " + addr,
		}
	case !served.answering && loginErr != nil:
		label, reason := describeStartFailure(loginErr)

		return ui.EngineStatus{
			Build:   "nothing, and its server could not start",
			Label:   label,
			Refused: reason,
		}
	case !served.answering:
		return ui.EngineStatus{Build: "nothing yet, the server starts on open"}
	case !served.current && setup.remote.IsRemote():
		return ui.EngineStatus{
			Build: version.Describe(served.version, served.commit),
			Label: "mismatch",
			Refused: fmt.Sprintf("the engine runs other code than this komrad, %s: %s",
				version.Describe(version.Version(), version.Commit()),
				doctor.StaleEngineFix(served.version, served.commit)),
		}
	case loginErr != nil:
		_, reason := describeStartFailure(loginErr)

		return ui.EngineStatus{Build: version.Describe(served.version, served.commit) + ", but it could not start again: " + reason}
	default:
		return ui.EngineStatus{Build: version.Describe(served.version, served.commit)}
	}
}

// describeStartFailure names why an engine's server could not start with the database url its file holds.
func describeStartFailure(err error) (string, string) {
	switch {
	case errors.Is(err, store.ErrNotAnEngine):
		return "no engine", "its database url logs in as no engine: " + err.Error()
	case errors.Is(err, db.ErrSchemaBehind), errors.Is(err, db.ErrSchemaAhead):
		return "schema", err.Error()
	default:
		return "login", "its database login fails: " + err.Error()
	}
}
