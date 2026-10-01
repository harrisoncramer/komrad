package server

import (
	"log/slog"
	"net/http"

	"komrad/internal/doctor"
	"komrad/internal/interceptors"
	"komrad/internal/jobs"
	"komrad/internal/logread"
	"komrad/internal/mcp"
	rpcagent "komrad/internal/rpc/agent"
	rpcconfig "komrad/internal/rpc/config"
	rpcdoctor "komrad/internal/rpc/doctor"
	rpcforge "komrad/internal/rpc/forge"
	rpcgit "komrad/internal/rpc/git"
	rpchook "komrad/internal/rpc/hook"
	rpclog "komrad/internal/rpc/log"
	rpcloop "komrad/internal/rpc/loop"
	rpcpullrequest "komrad/internal/rpc/pullrequest"
	rpcschedule "komrad/internal/rpc/schedule"
	rpcterminal "komrad/internal/rpc/terminal"
	rpcwatch "komrad/internal/rpc/watch"
	rpcworkflow "komrad/internal/rpc/workflow"
	rpcworktree "komrad/internal/rpc/worktree"
	"komrad/internal/rpccodec"
	"komrad/internal/watch"
	"komrad/proto/gen/agent/v1/agentv1connect"
	agentv1komrad "komrad/proto/gen/agent/v1/agentv1komrad"
	"komrad/proto/gen/config/v1/configv1connect"
	configv1komrad "komrad/proto/gen/config/v1/configv1komrad"
	"komrad/proto/gen/doctor/v1/doctorv1connect"
	doctorv1komrad "komrad/proto/gen/doctor/v1/doctorv1komrad"
	"komrad/proto/gen/forge/v1/forgev1connect"
	forgev1komrad "komrad/proto/gen/forge/v1/forgev1komrad"
	"komrad/proto/gen/git/v1/gitv1connect"
	gitv1komrad "komrad/proto/gen/git/v1/gitv1komrad"
	"komrad/proto/gen/hook/v1/hookv1connect"
	hookv1komrad "komrad/proto/gen/hook/v1/hookv1komrad"
	"komrad/proto/gen/log/v1/logv1connect"
	logv1komrad "komrad/proto/gen/log/v1/logv1komrad"
	"komrad/proto/gen/loop/v1/loopv1connect"
	loopv1komrad "komrad/proto/gen/loop/v1/loopv1komrad"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	pullrequestv1komrad "komrad/proto/gen/pullrequest/v1/pullrequestv1komrad"
	"komrad/proto/gen/schedule/v1/schedulev1connect"
	schedulev1komrad "komrad/proto/gen/schedule/v1/schedulev1komrad"
	"komrad/proto/gen/terminal/v1/terminalv1connect"
	terminalv1komrad "komrad/proto/gen/terminal/v1/terminalv1komrad"
	"komrad/proto/gen/watch/v1/watchv1connect"
	watchv1komrad "komrad/proto/gen/watch/v1/watchv1komrad"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	workflowv1komrad "komrad/proto/gen/workflow/v1/workflowv1komrad"
	"komrad/proto/gen/worktree/v1/worktreev1connect"
	worktreev1komrad "komrad/proto/gen/worktree/v1/worktreev1komrad"

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
	RunLogsDir    string
	// Doctor is this machine as the doctor's checks see it.
	Doctor doctor.Settings
	Watch  *watch.Broker
	Wakers *loopWakers
}

func newMux(params newMuxParams) (*http.ServeMux, error) {
	validation, err := interceptors.NewValidation()
	if err != nil {
		return nil, err
	}

	roleOf := newRoleOf(params.Stores.agents)
	options := []connect.HandlerOption{
		connect.WithInterceptors(interceptors.NewLogging(params.Logger), interceptors.NewRoles(roleOf), validation),
		interceptors.NewRecovery(),
		rpccodec.NewStrictJSON(),
	}

	mux := http.NewServeMux()
	worktreeDelete := jobs.NewWorktreeDelete(params.Services.queue)
	mux.Handle(HealthPath, newHealthHandler(params.Stores.pool.GetRawPool()))
	mux.Handle(StopPath, interceptors.NewLoggingHandler(params.Logger, newStopHandler(params.StopRequested)))
	mux.Handle(agentv1connect.NewAgentServiceHandler(
		agentv1komrad.AgentServiceAdapter{Impl: rpcagent.NewHandler(rpcagent.NewHandlerParams{
			Agents:        params.Services.rowReader,
			Rows:          params.Services.rowReader,
			Writer:        params.Services.rowWriter,
			Transcripts:   params.Services.transcriptReader,
			Screens:       params.Services.screens,
			Mail:          params.Services.mail,
			Creator:       params.Services.launch,
			Adopter:       params.Services.adopt,
			Liveness:      params.Services.liveness,
			Lifecycle:     params.Services.lifecycle,
			Resumer:       params.Services.resume,
			Holder:        params.Services.holder,
			Deleter:       params.Services.agentDelete,
			Review:        params.Services.review,
			Documents:     params.Services.documents,
			DocumentTypes: params.Services.documents,
			Rebaser:       params.Services.checkout,
			Pusher:        params.Services.checkout,
			Orchestrator:  params.Services.orchestrator,
		})}, options...))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(
		workflowv1komrad.WorkflowServiceAdapter{
			Impl: rpcworkflow.NewHandler(rpcworkflow.NewHandlerParams{
				Workflows: params.Services.workflow,
			}),
		}, options...))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(
		schedulev1komrad.ScheduleServiceAdapter{
			Impl: rpcschedule.NewHandler(rpcschedule.NewHandlerParams{
				Schedules: params.Services.schedule,
			}),
		}, options...))
	mux.Handle(configv1connect.NewConfigServiceHandler(
		configv1komrad.ConfigServiceAdapter{Impl: rpcconfig.NewHandler(params.Services.loadConfig)}, options...))
	mux.Handle(hookv1connect.NewHookServiceHandler(
		hookv1komrad.HookServiceAdapter{
			Impl: rpchook.NewHandler(params.Services.script),
		}, options...))
	mux.Handle(forgev1connect.NewForgeServiceHandler(
		forgev1komrad.ForgeServiceAdapter{
			Impl: rpcforge.NewHandler(params.Services.forge, params.Services.writer),
		}, options...))
	mux.Handle(pullrequestv1connect.NewPullRequestServiceHandler(
		pullrequestv1komrad.PullRequestServiceAdapter{
			Impl: rpcpullrequest.NewHandler(rpcpullrequest.NewHandlerParams{
				PullRequests: params.Services.pullRequests,
				Dismisser:    params.Services.dismisser,
			}),
		}, options...))
	mux.Handle(worktreev1connect.NewWorktreeServiceHandler(
		worktreev1komrad.WorktreeServiceAdapter{
			Impl: rpcworktree.NewHandler(rpcworktree.NewHandlerParams{
				Lister:    params.Services.worktreeLister,
				Inspector: params.Services.inspector,
				Remover:   params.Services.remover,
				Deleter:   worktreeDelete,
				Claimed:   worktreeDelete,
			}),
		}, options...))
	mux.Handle(watchv1connect.NewWatchServiceHandler(
		watchv1komrad.WatchServiceAdapter{
			Impl: rpcwatch.NewHandler(rpcwatch.NewHandlerParams{Subscriber: rpcwatch.BrokerSubscriber{Broker: params.Watch}}),
		}, options...))
	mux.Handle(terminalv1connect.NewTerminalServiceHandler(
		terminalv1komrad.TerminalServiceAdapter{
			Impl: rpcterminal.NewHandler(rpcterminal.NewHandlerParams{
				Shells:   params.Services.shells,
				Attaches: params.Services.attaches,
			}),
		}, options...))
	mux.Handle(gitv1connect.NewGitServiceHandler(
		gitv1komrad.GitServiceAdapter{
			Impl: rpcgit.NewHandler(rpcgit.NewHandlerParams{Git: params.Services.agentGit}),
		}, options...))
	mux.Handle(logv1connect.NewLogServiceHandler(
		logv1komrad.LogServiceAdapter{
			Impl: rpclog.NewHandler(logread.New(params.LogPath), logread.NewRuns(params.RunLogsDir)),
		}, options...))
	mux.Handle(loopv1connect.NewLoopServiceHandler(
		loopv1komrad.LoopServiceAdapter{
			Impl: rpcloop.NewHandler(rpcloop.NewHandlerParams{
				CwdSync:         params.Wakers.cwdSync,
				TranscriptSync:  params.Wakers.transcriptSync,
				PullRequestSync: params.Wakers.pullRequestSync,
			}),
		}, options...))
	mux.Handle(doctorv1connect.NewDoctorServiceHandler(
		doctorv1komrad.DoctorServiceAdapter{
			Impl: rpcdoctor.NewHandler(rpcdoctor.NewHandlerParams{
				Env:    doctor.NewEnv(params.Doctor),
				Checks: doctor.EngineChecks(params.Doctor),
			}),
		}, options...))

	mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{
		DB:      params.ReadOnly,
		BaseURL: "http://" + params.Addr,
		RoleOf:  roleOf,
		Logger:  params.Logger,
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", interceptors.NewLoggingHandler(params.Logger, mcpHandler))

	return mux, nil
}
