package server

import (
	"log/slog"
	"net/http"

	"komrad/internal/doctor"
	"komrad/internal/harness"
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
	rpctask "komrad/internal/rpc/task"
	rpcterminal "komrad/internal/rpc/terminal"
	rpcwatch "komrad/internal/rpc/watch"
	rpcworkflow "komrad/internal/rpc/workflow"
	rpcworktree "komrad/internal/rpc/worktree"
	"komrad/internal/rpccodec"
	"komrad/internal/watch"
	agentv1komrad "komrad/proto/gen/agent/v1/agentv1komrad"
	configv1komrad "komrad/proto/gen/config/v1/configv1komrad"
	doctorv1komrad "komrad/proto/gen/doctor/v1/doctorv1komrad"
	forgev1komrad "komrad/proto/gen/forge/v1/forgev1komrad"
	gitv1komrad "komrad/proto/gen/git/v1/gitv1komrad"
	hookv1komrad "komrad/proto/gen/hook/v1/hookv1komrad"
	logv1komrad "komrad/proto/gen/log/v1/logv1komrad"
	loopv1komrad "komrad/proto/gen/loop/v1/loopv1komrad"
	pullrequestv1komrad "komrad/proto/gen/pullrequest/v1/pullrequestv1komrad"
	schedulev1komrad "komrad/proto/gen/schedule/v1/schedulev1komrad"
	taskv1komrad "komrad/proto/gen/task/v1/taskv1komrad"
	terminalv1komrad "komrad/proto/gen/terminal/v1/terminalv1komrad"
	watchv1komrad "komrad/proto/gen/watch/v1/watchv1komrad"
	workflowv1komrad "komrad/proto/gen/workflow/v1/workflowv1komrad"
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
	Doctor    doctor.Settings
	Watch     *watch.Broker
	Wakers    *loopWakers
	Harnesses *harness.Registry
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

	svc := params.Services
	mux := http.NewServeMux()
	worktreeDelete := jobs.NewWorktreeDelete(svc.queue)
	mux.Handle(HealthPath, newHealthHandler(params.Stores.pool.GetRawPool()))
	mux.Handle(StopPath, interceptors.NewLoggingHandler(params.Logger, newStopHandler(params.StopRequested)))
	agentv1komrad.RegisterAgentService(mux, rpcagent.NewHandler(rpcagent.NewHandlerParams{
		Agents:        svc.agents.rowReader,
		Rows:          svc.agents.rowReader,
		Writer:        svc.agents.rowWriter,
		Transcripts:   svc.transcripts.reader,
		Screens:       svc.agents.screens,
		Mail:          svc.mail.service,
		Creator:       svc.agents.launch,
		Adopter:       svc.agents.adopt,
		Lifecycle:     svc.agents.lifecycle,
		Resumer:       svc.agents.resume,
		Holder:        svc.agents.holder,
		Deleter:       svc.agents.lifecycle,
		Review:        svc.review,
		ForgeReplies:  svc.pullRequests.forgeReply,
		Documents:     svc.documents,
		DocumentTypes: svc.documents,
		Rebaser:       svc.checkouts.service,
		Pusher:        svc.checkouts.service,
		Aborter:       svc.checkouts.service,
		Orchestrator:  svc.agents.orchestrator,
		HooksOf:       params.Harnesses.HooksOf,
	}), options...)
	workflowv1komrad.RegisterWorkflowService(mux, rpcworkflow.NewHandler(rpcworkflow.NewHandlerParams{
		Workflows: svc.agents.workflow,
	}), options...)
	schedulev1komrad.RegisterScheduleService(mux, rpcschedule.NewHandler(rpcschedule.NewHandlerParams{
		Schedules: svc.schedule,
	}), options...)
	taskv1komrad.RegisterTaskService(mux, rpctask.NewHandler(rpctask.NewHandlerParams{
		Tasks:    svc.tasks,
		Archiver: svc.agents.lifecycle,
		Deleter:  svc.agents.lifecycle,
	}), options...)
	configv1komrad.RegisterConfigService(mux, rpcconfig.NewHandler(svc.loadConfig), options...)
	hookv1komrad.RegisterHookService(mux, rpchook.NewHandler(svc.script), options...)
	forgev1komrad.RegisterForgeService(mux, rpcforge.NewHandler(svc.pullRequests.forge, svc.pullRequests.writer), options...)
	pullrequestv1komrad.RegisterPullRequestService(mux, rpcpullrequest.NewHandler(rpcpullrequest.NewHandlerParams{
		PullRequests: svc.pullRequests.lister,
	}), options...)
	worktreev1komrad.RegisterWorktreeService(mux, rpcworktree.NewHandler(rpcworktree.NewHandlerParams{
		Lister:    svc.worktrees.lister,
		Inspector: svc.worktrees.inspector,
		Remover:   svc.worktrees.remover,
		Deleter:   worktreeDelete,
	}), options...)
	watchv1komrad.RegisterWatchService(mux, rpcwatch.NewHandler(rpcwatch.NewHandlerParams{
		Subscriber: rpcwatch.BrokerSubscriber{Broker: params.Watch},
	}), options...)
	terminalv1komrad.RegisterTerminalService(mux, rpcterminal.NewHandler(rpcterminal.NewHandlerParams{
		Shells:   svc.terminal.shells,
		Attaches: svc.terminal.attaches,
	}), options...)
	gitv1komrad.RegisterGitService(mux, rpcgit.NewHandler(rpcgit.NewHandlerParams{Git: svc.agentGit}), options...)
	logv1komrad.RegisterLogService(mux, rpclog.NewHandler(logread.New(params.LogPath), logread.NewRuns(params.RunLogsDir), svc.tasks), options...)
	loopv1komrad.RegisterLoopService(mux, rpcloop.NewHandler(rpcloop.NewHandlerParams{
		CwdSync:         params.Wakers.cwdSync,
		TranscriptSync:  params.Wakers.transcriptSync,
		PullRequestSync: svc.pullRequests.sync,
	}), options...)
	doctorv1komrad.RegisterDoctorService(mux, rpcdoctor.NewHandler(rpcdoctor.NewHandlerParams{
		Env:    doctor.NewEnv(params.Doctor),
		Checks: doctor.EngineChecks(params.Doctor),
	}), options...)

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
