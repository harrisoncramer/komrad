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

	"komrad/config"
	"komrad/db"
	"komrad/internal/checkouts"
	"komrad/internal/doctor"
	"komrad/internal/documents"
	"komrad/internal/events"
	"komrad/internal/forge"
	"komrad/internal/harness"
	"komrad/internal/interceptors"
	"komrad/internal/jobs"
	"komrad/internal/logging"
	"komrad/internal/mail"
	"komrad/internal/pullrequests"
	"komrad/internal/reviews"
	"komrad/internal/store"
	"komrad/internal/tmux"
	"komrad/internal/watch"
	"komrad/internal/watched"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// NewServerParams is what the server is built over.
type NewServerParams struct {
	Env                *config.Environment
	ConfigStack        config.Stack
	Logger             *slog.Logger
	Addr               string
	Tmux               *tmux.Client
	Harnesses          *harness.Registry
	DefaultHarness     string
	DefaultFlags       map[string]string
	TrustDirs          map[string]bool
	Usage              config.Usage
	ContextPath        string
	HookCommand        string
	FallbackResumeRoot string
	Doctor             doctor.Settings
	ForgeBuilders      map[string]forge.Builder
}

type Server struct {
	Mux           *http.ServeMux
	Jobs          *river.Client[pgx.Tx]
	Pool          *store.Pool
	Delivery      *mail.DeliveryService
	Loops         []*loop
	Watch         *watch.Broker
	logger        *slog.Logger
	stopRequested chan struct{}
	background    *backgroundWork
	closers       []func()
}

// NewServer builds the server, with nothing listening and no loop running until the caller
// says so.
func NewServer(ctx context.Context, params NewServerParams) (*Server, error) {
	if err := harness.RequirePOSIXShell(params.Tmux.GetDefaultShell()); err != nil {
		return nil, err
	}

	st, err := openStores(ctx, params.Env.DatabaseURL)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		Pool:          st.pool,
		logger:        params.Logger,
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

	wakers := newLoopWakers()

	svc, err := buildServices(params, st, wakers)
	if err != nil {
		return nil, err
	}
	srv.Delivery = svc.mail.delivery

	broker := watch.NewBroker(ctx, watch.NewBrokerParams{
		Logger:    params.Logger,
		Pool:      st.pool.GetRawPool(),
		Rows:      svc.watchRows,
		Resources: watched.All(),
	})
	srv.Watch = broker
	srv.closers = append(srv.closers, broker.Close)

	if err := db.NewRiverMigrator(st.pool.GetRawPool()).Migrate(ctx, st.pool.GetRiverSchema()); err != nil {
		return nil, err
	}

	srv.Jobs, err = jobs.NewClient(jobs.NewClientParams{
		Pool:          st.pool.GetRawPool(),
		Store:         st.pool,
		Schema:        st.pool.GetRiverSchema(),
		Schedules:     st.schedules,
		Runs:          st.schedules,
		Launcher:      svc.agents.launch,
		Deleter:       svc.agents.lifecycle,
		Running:       st.agents,
		Orchestrators: svc.schedule,
		Mailer:        svc.mail.service,
		Registrations: svc.schedule,
		Worktrees:     svc.worktrees.lister,
		Remover:       svc.worktrees.remover,
		Resumer:       svc.agents.resume,
		Forges:        svc.pullRequests.forge,
		MaxLogBytes:   svc.getMaxCheckLogBytes,
		Router: events.NewRouter(events.NewRouterParams{
			Mailer: svc.mail.service,
			Handlers: []events.Handler{
				pullrequests.NewEventHandler(),
				documents.NewEventHandler(),
				checkouts.NewEventHandler(),
				checkouts.NewPushedHandler(),
				checkouts.NewAbortedHandler(),
				reviews.NewReleasedHandler(),
				reviews.NewAnsweredHandler(),
			},
		}),
		Logger: params.Logger,
	})
	if err != nil {
		return nil, err
	}

	// The MCP query tools connect through a least-privilege role, so their pool is opened
	// apart from the one everything else writes through.
	readOnly, err := store.OpenReadOnly(ctx, params.Env.ReadOnlyDatabaseURL, st.pool.GetEngine().ID)
	if err != nil {
		return nil, err
	}
	srv.closers = append(srv.closers, readOnly.Close)

	srv.Mux, err = newMux(newMuxParams{
		Logger:        params.Logger,
		Addr:          params.Addr,
		Stores:        st,
		Services:      svc,
		Jobs:          srv.Jobs,
		ReadOnly:      readOnly,
		StopRequested: srv.stopRequested,
		LogPath:       params.Env.LogPath(),
		RunLogsDir:    params.Env.RunLogsDir(),
		Doctor:        params.Doctor,
		Watch:         broker,
		Wakers:        wakers,
	})
	if err != nil {
		return nil, err
	}

	if err := svc.agents.delete.Resume(ctx); err != nil {
		return nil, err
	}

	if err := svc.pullRequests.forgeReply.ReconcileForgeReplies(ctx); err != nil {
		params.Logger.LogAttrs(ctx, slog.LevelWarn, "failed to settle the forge replies komrad stopped while posting", logging.Err(err)...)
	}

	if err := svc.schedule.ArmPeriodic(ctx, jobs.NewRiverPeriodic(srv.Jobs.PeriodicJobs())); err != nil {
		return nil, err
	}

	srv.Loops = buildLoops(buildLoopsParams{
		Stores:   st,
		Services: svc,
		Claimed:  jobs.NewWorktreeDelete(svc.queue),
		Wakers:   wakers,
	})

	if err := broker.Start(ctx); err != nil {
		return nil, err
	}

	built = true

	return srv, nil
}

// mailConfig reads the mail settings fresh on every send, and fails rather than falling back to
// defaults.
type mailConfig func() (*config.Config, error)

func (c mailConfig) GetDeliveryDeadline() (time.Duration, error) {
	cfg, err := c()
	if err != nil {
		return 0, fmt.Errorf("failed to read the config: %w", err)
	}

	return cfg.Mail.GetDeliveryDeadline()
}

func (c mailConfig) GetMaxExchange() (int, error) {
	cfg, err := c()
	if err != nil {
		return 0, fmt.Errorf("failed to read the config: %w", err)
	}

	return cfg.Mail.GetMaxExchange()
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
