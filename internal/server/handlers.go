package server

import (
	"log/slog"
	"net/http"

	"komrade/internal/doctor"
	"komrade/internal/interceptors"
	"komrade/internal/jobs"
	"komrade/internal/logread"
	"komrade/internal/mcp"
	rpcagent "komrade/internal/rpc/agent"
	rpcconfig "komrade/internal/rpc/config"
	rpcdoctor "komrade/internal/rpc/doctor"
	rpcforge "komrade/internal/rpc/forge"
	rpchook "komrade/internal/rpc/hook"
	rpclog "komrade/internal/rpc/log"
	rpcpullrequest "komrade/internal/rpc/pullrequest"
	rpcschedule "komrade/internal/rpc/schedule"
	rpcwatch "komrade/internal/rpc/watch"
	rpcworkflow "komrade/internal/rpc/workflow"
	rpcworktree "komrade/internal/rpc/worktree"
	"komrade/internal/rpccodec"
	"komrade/internal/watch"
	"komrade/proto/gen/agent/v1/agentv1connect"
	agentv1komrade "komrade/proto/gen/agent/v1/agentv1komrade"
	"komrade/proto/gen/config/v1/configv1connect"
	configv1komrade "komrade/proto/gen/config/v1/configv1komrade"
	"komrade/proto/gen/doctor/v1/doctorv1connect"
	doctorv1komrade "komrade/proto/gen/doctor/v1/doctorv1komrade"
	"komrade/proto/gen/forge/v1/forgev1connect"
	forgev1komrade "komrade/proto/gen/forge/v1/forgev1komrade"
	"komrade/proto/gen/hook/v1/hookv1connect"
	hookv1komrade "komrade/proto/gen/hook/v1/hookv1komrade"
	"komrade/proto/gen/log/v1/logv1connect"
	logv1komrade "komrade/proto/gen/log/v1/logv1komrade"
	"komrade/proto/gen/pullrequest/v1/pullrequestv1connect"
	pullrequestv1komrade "komrade/proto/gen/pullrequest/v1/pullrequestv1komrade"
	"komrade/proto/gen/schedule/v1/schedulev1connect"
	schedulev1komrade "komrade/proto/gen/schedule/v1/schedulev1komrade"
	"komrade/proto/gen/watch/v1/watchv1connect"
	watchv1komrade "komrade/proto/gen/watch/v1/watchv1komrade"
	"komrade/proto/gen/workflow/v1/workflowv1connect"
	workflowv1komrade "komrade/proto/gen/workflow/v1/workflowv1komrade"
	"komrade/proto/gen/worktree/v1/worktreev1connect"
	worktreev1komrade "komrade/proto/gen/worktree/v1/worktreev1komrade"

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
	// Doctor is this machine as the doctor's checks see it.
	Doctor doctor.Settings
	Watch  *watch.Broker
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
	queue := jobs.NewEnqueuer(params.Jobs, params.Stores.pool.GetRawPool())
	worktreeDelete := jobs.NewWorktreeDelete(queue)
	mux.Handle(HealthPath, newHealthHandler(params.Stores.pool.GetRawPool()))
	mux.Handle(StopPath, interceptors.NewLoggingHandler(params.Logger, newStopHandler(params.StopRequested)))
	mux.Handle(agentv1connect.NewAgentServiceHandler(
		agentv1komrade.AgentServiceAdapter{Impl: rpcagent.NewHandler(rpcagent.NewHandlerParams{
			Agents:       params.Services.rowReader,
			Rows:         params.Services.rowReader,
			Writer:       params.Services.rowWriter,
			Transcripts:  params.Services.transcriptReader,
			Mail:         params.Services.mail,
			Creator:      params.Services.launch,
			Adopter:      params.Services.adopt,
			Liveness:     params.Services.liveness,
			Lifecycle:    params.Services.lifecycle,
			Resumer:      params.Services.resume,
			Holder:       params.Services.holder,
			Archive:      jobs.NewArchiveSweep(queue),
			Review:       params.Services.review,
			Marks:        params.Services.marks,
			Rebaser:      params.Services.rebase,
			Orchestrator: params.Services.orchestrator,
		})}, options...))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(
		workflowv1komrade.WorkflowServiceAdapter{
			Impl: rpcworkflow.NewHandler(rpcworkflow.NewHandlerParams{
				Workflows: params.Services.workflow,
			}),
		}, options...))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(
		schedulev1komrade.ScheduleServiceAdapter{
			Impl: rpcschedule.NewHandler(rpcschedule.NewHandlerParams{
				Schedules: params.Services.schedule,
			}),
		}, options...))
	mux.Handle(configv1connect.NewConfigServiceHandler(
		configv1komrade.ConfigServiceAdapter{Impl: rpcconfig.NewHandler(params.Services.loadConfig)}, options...))
	mux.Handle(hookv1connect.NewHookServiceHandler(
		hookv1komrade.HookServiceAdapter{
			Impl: rpchook.NewHandler(params.Services.script),
		}, options...))
	mux.Handle(forgev1connect.NewForgeServiceHandler(
		forgev1komrade.ForgeServiceAdapter{
			Impl: rpcforge.NewHandler(params.Services.forge),
		}, options...))
	mux.Handle(pullrequestv1connect.NewPullRequestServiceHandler(
		pullrequestv1komrade.PullRequestServiceAdapter{
			Impl: rpcpullrequest.NewHandler(rpcpullrequest.NewHandlerParams{PullRequests: params.Services.pullRequests}),
		}, options...))
	mux.Handle(worktreev1connect.NewWorktreeServiceHandler(
		worktreev1komrade.WorktreeServiceAdapter{
			Impl: rpcworktree.NewHandler(rpcworktree.NewHandlerParams{
				Lister:    params.Services.worktreeLister,
				Inspector: params.Services.inspector,
				Remover:   params.Services.remover,
				Deleter:   worktreeDelete,
				Claimed:   worktreeDelete,
			}),
		}, options...))
	mux.Handle(watchv1connect.NewWatchServiceHandler(
		watchv1komrade.WatchServiceAdapter{
			Impl: rpcwatch.NewHandler(rpcwatch.NewHandlerParams{Subscriber: rpcwatch.BrokerSubscriber{Broker: params.Watch}}),
		}, options...))
	mux.Handle(logv1connect.NewLogServiceHandler(
		logv1komrade.LogServiceAdapter{
			Impl: rpclog.NewHandler(logread.New(params.LogPath)),
		}, options...))
	mux.Handle(doctorv1connect.NewDoctorServiceHandler(
		doctorv1komrade.DoctorServiceAdapter{
			Impl: rpcdoctor.NewHandler(rpcdoctor.NewHandlerParams{
				Env:    doctor.NewEnv(params.Doctor),
				Checks: doctor.EngineChecks(params.Doctor),
			}),
		}, options...))

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{
		DB:      params.ReadOnly,
		BaseURL: "http://" + params.Addr,
		RoleOf:  newRoleOf(params.Stores.agents, params.Logger),
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", interceptors.NewLoggingHandler(params.Logger, mcpHandler))

	return mux, nil
}
