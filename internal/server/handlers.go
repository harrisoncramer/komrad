package server

import (
	"log/slog"
	"net/http"

	"weave/internal/doctor"
	"weave/internal/interceptors"
	"weave/internal/jobs"
	"weave/internal/logread"
	"weave/internal/mcp"
	rpcagent "weave/internal/rpc/agent"
	rpcconfig "weave/internal/rpc/config"
	rpcdoctor "weave/internal/rpc/doctor"
	rpcforge "weave/internal/rpc/forge"
	rpchook "weave/internal/rpc/hook"
	rpclog "weave/internal/rpc/log"
	rpcpullrequest "weave/internal/rpc/pullrequest"
	rpcschedule "weave/internal/rpc/schedule"
	rpcworkflow "weave/internal/rpc/workflow"
	rpcworktree "weave/internal/rpc/worktree"
	"weave/internal/rpccodec"
	"weave/proto/gen/agent/v1/agentv1connect"
	agentv1weave "weave/proto/gen/agent/v1/agentv1weave"
	"weave/proto/gen/config/v1/configv1connect"
	configv1weave "weave/proto/gen/config/v1/configv1weave"
	"weave/proto/gen/doctor/v1/doctorv1connect"
	doctorv1weave "weave/proto/gen/doctor/v1/doctorv1weave"
	"weave/proto/gen/forge/v1/forgev1connect"
	forgev1weave "weave/proto/gen/forge/v1/forgev1weave"
	"weave/proto/gen/hook/v1/hookv1connect"
	hookv1weave "weave/proto/gen/hook/v1/hookv1weave"
	"weave/proto/gen/log/v1/logv1connect"
	logv1weave "weave/proto/gen/log/v1/logv1weave"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"
	pullrequestv1weave "weave/proto/gen/pullrequest/v1/pullrequestv1weave"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	schedulev1weave "weave/proto/gen/schedule/v1/schedulev1weave"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	workflowv1weave "weave/proto/gen/workflow/v1/workflowv1weave"
	"weave/proto/gen/worktree/v1/worktreev1connect"
	worktreev1weave "weave/proto/gen/worktree/v1/worktreev1weave"

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
		agentv1weave.AgentServiceAdapter{Impl: rpcagent.NewAgentService(rpcagent.NewAgentServiceParams{
			Agents:       params.Stores.agents,
			Rows:         params.Services.rows,
			Transcripts:  params.Stores.transcripts,
			Mail:         params.Services.mail,
			Creator:      params.Services.launch,
			Adopter:      params.Services.adopt,
			Liveness:     params.Services.liveness,
			Lifecycle:    params.Services.lifecycle,
			Resumer:      params.Services.resume,
			Holder:       params.Services.holder,
			Reconciler:   params.Services.reconciler,
			Archive:      jobs.NewArchiveSweep(queue),
			Review:       params.Services.review,
			Runs:         params.Stores.runs,
			Marks:        params.Services.marks,
			Rebaser:      params.Services.rebase,
			Orchestrator: params.Services.orchestrator,
		})}, options...))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(
		workflowv1weave.WorkflowServiceAdapter{
			Impl: rpcworkflow.NewWorkflowService(params.Stores.workflows, params.Services.workflow, params.Services.marks),
		}, options...))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(
		schedulev1weave.ScheduleServiceAdapter{
			Impl: rpcschedule.NewScheduleService(params.Stores.schedules, params.Services.schedule),
		}, options...))
	mux.Handle(configv1connect.NewConfigServiceHandler(
		configv1weave.ConfigServiceAdapter{Impl: rpcconfig.NewConfigService(params.Services.loadConfig)}, options...))
	mux.Handle(hookv1connect.NewHookServiceHandler(
		hookv1weave.HookServiceAdapter{
			Impl: rpchook.NewHookService(params.Services.script),
		}, options...))
	mux.Handle(forgev1connect.NewForgeServiceHandler(
		forgev1weave.ForgeServiceAdapter{
			Impl: rpcforge.NewForgeService(params.Services.forge),
		}, options...))
	mux.Handle(pullrequestv1connect.NewPullRequestServiceHandler(
		pullrequestv1weave.PullRequestServiceAdapter{
			Impl: rpcpullrequest.NewPullRequestService(rpcpullrequest.NewPullRequestServiceParams{
				Forges:           params.Services.forge,
				Agents:           params.Stores.agents,
				PullRequests:     params.Stores.pullRequests,
				PullRequestSyncs: params.Stores.pullRequestSyncs,
			}),
		}, options...))
	mux.Handle(worktreev1connect.NewWorktreeServiceHandler(
		worktreev1weave.WorktreeServiceAdapter{
			Impl: rpcworktree.NewWorktreeService(rpcworktree.NewWorktreeServiceParams{
				Lister:    params.Services.worktrees,
				Inspector: params.Services.inspector,
				Remover:   params.Services.remover,
				Deleter:   worktreeDelete,
				Claimed:   worktreeDelete,
			}),
		}, options...))
	mux.Handle(logv1connect.NewLogServiceHandler(
		logv1weave.LogServiceAdapter{
			Impl: rpclog.NewLogService(logread.New(params.LogPath)),
		}, options...))
	mux.Handle(doctorv1connect.NewDoctorServiceHandler(
		doctorv1weave.DoctorServiceAdapter{
			Impl: rpcdoctor.NewDoctorService(rpcdoctor.NewDoctorServiceParams{
				Env:    doctor.NewEnv(params.Doctor),
				Checks: doctor.EngineChecks(params.Doctor),
			}),
		}, options...))

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{DB: params.ReadOnly, BaseURL: "http://" + params.Addr})
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", interceptors.NewLoggingHandler(params.Logger, mcpHandler))

	return mux, nil
}
