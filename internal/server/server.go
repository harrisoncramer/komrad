// Package server assembles the running service: the stores over one pool, the domain services
// over those, the ConnectRPC and MCP handlers over those, and the background loops that watch
// every agent.
//
// It owns the wiring and the shutdown, so the pieces beneath it take their dependencies rather
// than reaching for them.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"weave/config"
	"weave/db"
	"weave/internal/agents"
	"weave/internal/browser"
	"weave/internal/harness"
	"weave/internal/interceptors"
	"weave/internal/jobs"
	"weave/internal/store"
	"weave/internal/tmux"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type Params struct {
	Env                *config.Environment
	ConfigPaths        []string
	Logger             *slog.Logger
	Addr               string
	Tmux               *tmux.Client
	Harnesses          *harness.Registry
	DefaultHarness     string
	DefaultFlags       map[string]string
	MaxAgents          int
	MaxChildren        int
	ContextPath        string
	HookCommand        string
	FallbackResumeRoot string
	// Opener is where a URL bound for the person at the panel goes, which a test supplies
	// itself so a run does not open windows on whoever is watching.
	Opener browser.Opener
}

// mailConfig reads the mail settings fresh on every send, and fails rather than falling back
// to defaults so a deadline somebody wrote is never quietly ignored.
type mailConfig func() (*config.Config, error)

func (load mailConfig) GetDeliveryDeadline() (time.Duration, error) {
	cfg, err := load()
	if err != nil {
		return 0, fmt.Errorf("failed to read the config: %w", err)
	}

	return cfg.Mail.GetDeliveryDeadline()
}

func (load mailConfig) GetMaxExchange() (int, error) {
	cfg, err := load()
	if err != nil {
		return 0, fmt.Errorf("failed to read the config: %w", err)
	}

	return cfg.Mail.GetMaxExchange()
}

type Server struct {
	Mux           *http.ServeMux
	Jobs          *river.Client[pgx.Tx]
	Periodic      *jobs.RiverPeriodic
	Pool          *store.Pool
	Delivery      *agents.DeliveryService
	Loops         []Loop
	logger        *slog.Logger
	stopRequested chan struct{}
	background    *backgroundWork
	closers       []func()
}

// New builds the service, with nothing listening and no loop running until the caller says
// so.
func New(ctx context.Context, p Params) (*Server, error) {
	if err := harness.RequirePOSIXShell(p.Tmux.GetDefaultShell()); err != nil {
		return nil, err
	}

	st, err := openStores(ctx, p.Env.DatabaseURL)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		Pool:          st.pool,
		logger:        p.Logger,
		stopRequested: make(chan struct{}, 1),
	}
	srv.closers = append(srv.closers, func() { _ = st.pool.Close() })

	// Every failure past this point leaves an open handle behind, so the unwind stays armed
	// until the built server is handed back.
	built := false
	defer func() {
		if !built {
			srv.Close()
		}
	}()

	svc, err := buildServices(p, st)
	if err != nil {
		return nil, err
	}
	srv.Delivery = svc.delivery

	if err := db.MigrateRiver(ctx, st.pool.GetRawPool()); err != nil {
		return nil, err
	}

	srv.Jobs, err = jobs.NewClient(jobs.ClientParams{
		Pool:      st.pool.GetRawPool(),
		Schedules: st.schedules,
		Results:   st.schedules,
		Launcher:  svc.launch,
		Deleter:   svc.lifecycle,
		Archived:  st.agents,
		Running:   st.agents,
		Logger:    p.Logger,
	})
	if err != nil {
		return nil, err
	}

	// The MCP query tools connect through a least-privilege role, so their pool is opened
	// apart from the one everything else writes through.
	readOnly, err := store.OpenReadOnly(ctx, p.Env.ReadOnlyDatabaseURL)
	if err != nil {
		return nil, err
	}
	srv.closers = append(srv.closers, readOnly.Close)

	srv.Mux, err = newMux(newMuxParams{
		Logger:        p.Logger,
		Addr:          p.Addr,
		Stores:        st,
		Services:      svc,
		Jobs:          srv.Jobs,
		ReadOnly:      readOnly,
		StopRequested: srv.stopRequested,
	})
	if err != nil {
		return nil, err
	}

	srv.Periodic = jobs.NewRiverPeriodic(srv.Jobs.PeriodicJobs())
	srv.Loops = buildLoops(buildLoopsParams{
		Harnesses: p.Harnesses,
		Stores:    st,
		Services:  svc,
		Periodic:  srv.Periodic,
	})

	built = true

	return srv, nil
}

// Handler is the mux behind the guard that refuses anything not from this machine.
func (s *Server) Handler() http.Handler {
	return interceptors.NewLocalGuard(s.logger, s.Mux)
}

// Close releases the database handles, in the reverse of the order they were opened.
func (s *Server) Close() {
	for i := len(s.closers) - 1; i >= 0; i-- {
		s.closers[i]()
	}

	s.closers = nil
}
