package server

import (
	"log/slog"
	"net/http"

	"weave/internal/interceptors"
	"weave/internal/jobs"
	"weave/internal/mcp"
	rpcaction "weave/internal/rpc/action"
	rpcagent "weave/internal/rpc/agent"
	rpcconfig "weave/internal/rpc/config"
	rpcschedule "weave/internal/rpc/schedule"
	rpcworkflow "weave/internal/rpc/workflow"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/config/v1/configv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

type newMuxParams struct {
	Logger        *slog.Logger
	Addr          string
	Stores        *stores
	Services      *services
	Jobs          *river.Client[pgx.Tx]
	ReadOnly      *pgxpool.Pool
	StopRequested chan struct{}
}

func newMux(p newMuxParams) (*http.ServeMux, error) {
	validation, err := interceptors.NewValidation()
	if err != nil {
		return nil, err
	}

	options := []connect.HandlerOption{
		connect.WithInterceptors(interceptors.NewLogging(p.Logger), validation),
		interceptors.NewRecovery(),
	}

	mux := http.NewServeMux()
	mux.Handle(HealthPath, newHealthHandler(p.Stores.pool.GetRawPool()))
	mux.Handle(StopPath, interceptors.NewLoggingHandler(p.Logger, newStopHandler(p.StopRequested)))
	mux.Handle(agentv1connect.NewAgentServiceHandler(
		rpcagent.NewService(rpcagent.NewServiceParams{
			Agents:      p.Stores.agents,
			Transcripts: p.Stores.transcripts,
			Mail:        p.Services.mail,
			Creator:     p.Services.launch,
			Adopter:     p.Services.adopt,
			Liveness:    p.Services.liveness,
			Lifecycle:   p.Services.lifecycle,
			Resumer:     p.Services.resume,
			Reconciler:  p.Services.reconciler,
			Archive:     jobs.NewEnqueuer(p.Jobs),
			Review:      p.Services.review,
		}), options...))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(
		rpcworkflow.NewService(p.Stores.workflows, p.Services.workflow), options...))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(
		rpcschedule.NewService(p.Stores.schedules, p.Services.schedule), options...))
	mux.Handle(configv1connect.NewConfigServiceHandler(
		rpcconfig.NewService(p.Services.loadConfig), options...))
	mux.Handle(actionv1connect.NewActionServiceHandler(
		rpcaction.NewService(p.Services.action), options...))

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{DB: p.ReadOnly, BaseURL: "http://" + p.Addr})
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", interceptors.NewLoggingHandler(p.Logger, mcpHandler))

	return mux, nil
}
