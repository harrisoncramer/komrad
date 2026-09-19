package server

import (
	"log/slog"
	"net/http"

	"weave/internal/interceptors"
	"weave/internal/jobs"
	"weave/internal/logread"
	"weave/internal/mcp"
	rpcagent "weave/internal/rpc/agent"
	rpcconfig "weave/internal/rpc/config"
	rpchook "weave/internal/rpc/hook"
	rpclog "weave/internal/rpc/log"
	rpcschedule "weave/internal/rpc/schedule"
	rpcworkflow "weave/internal/rpc/workflow"
	"weave/internal/rpccodec"
	"weave/proto/gen/agent/v1/agentv1connect"
	agentv1weave "weave/proto/gen/agent/v1/agentv1weave"
	"weave/proto/gen/config/v1/configv1connect"
	configv1weave "weave/proto/gen/config/v1/configv1weave"
	"weave/proto/gen/hook/v1/hookv1connect"
	hookv1weave "weave/proto/gen/hook/v1/hookv1weave"
	"weave/proto/gen/log/v1/logv1connect"
	logv1weave "weave/proto/gen/log/v1/logv1weave"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	schedulev1weave "weave/proto/gen/schedule/v1/schedulev1weave"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	workflowv1weave "weave/proto/gen/workflow/v1/workflowv1weave"

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
	LogPath       string
}

func newMux(params newMuxParams) (*http.ServeMux, error) {
	validation, err := interceptors.NewValidation()
	if err != nil {
		return nil, err
	}

	options := []connect.HandlerOption{
		connect.WithInterceptors(interceptors.NewLogging(params.Logger), validation),
		interceptors.NewRecovery(),
		rpccodec.NewStrictJSON(),
	}

	mux := http.NewServeMux()
	mux.Handle(HealthPath, newHealthHandler(params.Stores.pool.GetRawPool()))
	mux.Handle(StopPath, interceptors.NewLoggingHandler(params.Logger, newStopHandler(params.StopRequested)))
	mux.Handle(agentv1connect.NewAgentServiceHandler(
		agentv1weave.AgentServiceAdapter{Impl: rpcagent.NewService(rpcagent.NewServiceParams{
			Agents:       params.Stores.agents,
			Transcripts:  params.Stores.transcripts,
			Mail:         params.Services.mail,
			Creator:      params.Services.launch,
			Adopter:      params.Services.adopt,
			Liveness:     params.Services.liveness,
			Trees:        params.Services.cwdSync,
			Lifecycle:    params.Services.lifecycle,
			Resumer:      params.Services.resume,
			Holder:       params.Services.holder,
			Reconciler:   params.Services.reconciler,
			Archive:      jobs.NewEnqueuer(params.Jobs),
			Review:       params.Services.review,
			Runs:         params.Stores.runs,
			Marks:        params.Services.marks,
			Refresher:    params.Services.rebase,
			Orchestrator: params.Services.orchestrator,
		})}, options...))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(
		workflowv1weave.WorkflowServiceAdapter{
			Impl: rpcworkflow.NewService(params.Stores.workflows, params.Services.workflow, params.Services.marks),
		}, options...))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(
		schedulev1weave.ScheduleServiceAdapter{
			Impl: rpcschedule.NewService(params.Stores.schedules, params.Services.schedule),
		}, options...))
	mux.Handle(configv1connect.NewConfigServiceHandler(
		configv1weave.ConfigServiceAdapter{Impl: rpcconfig.NewService(params.Services.loadConfig)}, options...))
	mux.Handle(hookv1connect.NewHookServiceHandler(
		hookv1weave.HookServiceAdapter{
			Impl: rpchook.NewService(params.Services.script),
		}, options...))
	mux.Handle(logv1connect.NewLogServiceHandler(
		logv1weave.LogServiceAdapter{
			Impl: rpclog.NewService(logread.New(params.LogPath)),
		}, options...))

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{DB: params.ReadOnly, BaseURL: "http://" + params.Addr})
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", interceptors.NewLoggingHandler(params.Logger, mcpHandler))

	return mux, nil
}
