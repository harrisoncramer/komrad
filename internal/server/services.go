package server

import (
	"fmt"
	"os"
	"path/filepath"

	"komrad/config"
	"komrad/internal/agentgit"
	"komrad/internal/agents"
	"komrad/internal/attaches"
	"komrad/internal/checkouts"
	"komrad/internal/documents"
	"komrad/internal/forge"
	"komrad/internal/forge/forges"
	"komrad/internal/frames"
	"komrad/internal/git"
	"komrad/internal/harness/pane"
	"komrad/internal/jobs"
	"komrad/internal/mail"
	"komrad/internal/pullrequests"
	"komrad/internal/reviews"
	"komrad/internal/schedules"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/shells"
	"komrad/internal/tasks"
	"komrad/internal/transcripts"
	"komrad/internal/watch"
	"komrad/internal/worktrees"
)

type services struct {
	loadConfig   func() (*config.Config, error)
	git          *git.Client
	queue        *jobs.Enqueuer
	script       *scriptrun.ScriptService
	agents       agentServices
	mail         mailServices
	review       *reviews.ReviewService
	schedule     *schedules.ScheduleService
	documents    *documents.DocumentService
	tasks        *tasks.TaskService
	transcripts  transcriptServices
	pullRequests pullRequestServices
	checkouts    checkoutServices
	worktrees    worktreeServices
	terminal     terminalServices
	agentGit     *agentgit.GitService
	watchRows    *watch.Rows
}

type agentServices struct {
	liveness     *agents.PaneLiveness
	screens      *agents.ScreenKeeper
	launch       *agents.LaunchService
	resume       *agents.ResumeService
	adopt        *agents.AdoptService
	lifecycle    *agents.LifecycleService
	workflow     *agents.WorkflowService
	orchestrator *agents.OrchestratorService
	holder       *agents.Holder
	reconciler   *agents.Reconciler
	rowReader    *agents.RowReader
	rowWriter    *agents.RowWriter
	delete       *jobs.AgentDelete
}

type mailServices struct {
	service  *mail.MailService
	delivery *mail.DeliveryService
}

type transcriptServices struct {
	reader *transcripts.TranscriptReader
	sync   *transcripts.TranscriptSyncer
}

type pullRequestServices struct {
	forge       *forge.Service
	sync        *pullrequests.PullRequestSyncer
	commentSync *pullrequests.CommentSyncer
	forgeReply  *reviews.ForgeReplyService
	lister      *pullrequests.Lister
	writer      *pullrequests.Writer
}

type checkoutServices struct {
	service *checkouts.Service
	cwdSync *checkouts.CwdSyncer
}

type worktreeServices struct {
	lister    *worktrees.Lister
	checkouts *worktrees.CheckoutReader
	inspector *worktrees.Inspector
	remover   *worktrees.Remover
}

type terminalServices struct {
	shells   *shells.ShellService
	attaches *attaches.AttachService
}

// BuiltinScriptsDir is where the scripts komrad ships are installed under a cache directory,
// which is the only place a script that is not a hook is resolved from.
func BuiltinScriptsDir(cacheDir string) string {
	return filepath.Join(cacheDir, "komrad-builtin-scripts")
}

func buildServices(params NewServerParams, st *stores, wakers *loopWakers) (*services, error) {
	loadConfig := config.NewStackLoader(params.ConfigStack).Load
	gitClient := git.NewClient(git.NewClientParams{SSHHosts: func() (map[string]string, error) {
		cfg, err := loadConfig()
		if err != nil {
			return nil, err
		}

		return cfg.Git.GetSSHHosts()
	}})

	runLogsDir := params.Env.RunLogsDir()
	scriptService, err := buildScripts(buildScriptsParams{
		Server:     params,
		Stores:     st,
		LoadConfig: loadConfig,
		RunLogsDir: runLogsDir,
	})
	if err != nil {
		return nil, err
	}

	inserter, err := jobs.NewInsertOnlyClient(st.pool.GetRawPool(), st.pool.GetRiverSchema())
	if err != nil {
		return nil, err
	}
	queue := jobs.NewEnqueuer(inserter, st.pool.GetRawPool())

	paneChannel := pane.NewPane(pane.NewPaneParams{Terminal: params.Tmux, Harnesses: params.Harnesses})
	settler := agents.NewSettler(agents.NewSettlerParams{
		Channel:  paneChannel,
		Observer: paneChannel,
	})
	taskService := tasks.NewTaskService(tasks.NewTaskServiceParams{Tasks: st.tasks, Agents: st.agents})
	agentGroup := buildAgents(buildAgentsParams{
		Tasks:      taskService,
		Server:     params,
		Stores:     st,
		Wakers:     wakers,
		Git:        gitClient,
		Queue:      queue,
		Scripts:    scriptService,
		Panes:      paneChannel,
		Settler:    settler,
		RunLogsDir: runLogsDir,
		LoadConfig: loadConfig,
	})
	pullRequestGroup := buildPullRequests(buildPullRequestsParams{
		Server:     params,
		Stores:     st,
		Git:        gitClient,
		Queue:      queue,
		Wakers:     wakers,
		LoadConfig: loadConfig,
	})
	worktreeGroup := buildWorktrees(st, gitClient, loadConfig)

	return &services{
		loadConfig: loadConfig,
		queue:      queue,
		script:     scriptService,
		agents:     agentGroup,
		mail: mailServices{
			service: mail.NewMailService(mail.NewMailServiceParams{
				Mail:      st.mail,
				Agents:    st.agents,
				Workflows: st.workflows,
				Config:    mailConfig(loadConfig),
			}),
			delivery: mail.NewDeliveryService(mail.NewDeliveryServiceParams{
				Mail:         st.mail,
				NudgeClaimer: st.agents,
				PaneReader:   agentGroup.liveness,
				Channel:      paneChannel,
			}),
		},
		review: reviews.NewReviewService(reviews.NewReviewServiceParams{
			Reviews:   st.review,
			Snapshots: gitClient,
			Workflows: st.workflows,
			Agents:    st.agents,
			Tasks:     st.tasks,
			Events:    queue,
			StateDir:  params.Env.StateDir,
			Interrupter: agents.NewInterrupter(agents.NewInterrupterParams{
				Agents:   st.agents,
				Channel:  paneChannel,
				Observer: paneChannel,
			}),
		}),
		schedule: schedules.NewScheduleService(schedules.NewScheduleServiceParams{
			Schedules: st.schedules,
			Workflows: st.workflows,
			Agents:    st.agents,
		}),
		tasks: taskService,
		documents: documents.NewDocumentService(documents.NewDocumentServiceParams{
			Agents:    st.agents,
			Documents: st.documents,
			Types:     st.documentTypes,
			Events:    queue,
			Tasks:     taskService,
		}),
		transcripts: transcriptServices{
			reader: transcripts.NewTranscriptReader(transcripts.NewTranscriptReaderParams{
				Agents:      st.agents,
				Transcripts: st.transcripts,
				Tasks:       st.tasks,
			}),
			sync: transcripts.NewTranscriptSyncer(transcripts.NewTranscriptSyncerParams{
				Agents:      st.agents,
				Transcripts: st.transcripts,
				Harnesses:   params.Harnesses,
			}),
		},
		pullRequests: pullRequestGroup,
		git:          gitClient,
		checkouts: checkoutServices{
			service: checkouts.NewService(checkouts.NewServiceParams{
				Agents:          st.agents,
				Tasks:           st.tasks,
				Workflows:       st.workflows,
				Settler:         settler,
				Events:          queue,
				Git:             gitClient,
				PullRequests:    st.pullRequests,
				CheckoutSync:    wakers.cwdSync,
				PullRequestSync: pullRequestGroup.sync,
			}),
			cwdSync: checkouts.NewCwdSyncer(checkouts.NewCwdSyncerParams{
				Tasks:        st.tasks,
				Heads:        gitClient,
				Trees:        gitClient,
				Checkouts:    worktreeGroup.checkouts,
				Rounds:       st.review,
				Workflows:    st.workflows,
				PullRequests: st.pullRequests,
				Worktrees:    st.worktrees,
			}),
		},
		worktrees: worktreeGroup,
		terminal: terminalServices{
			shells: shells.NewShellService(shells.NewShellServiceParams{
				Agents: st.agents,
				Shell:  params.Env.Shell,
			}),
			attaches: attaches.NewAttachService(attaches.NewAttachServiceParams{
				Agents:  st.agents,
				Tmux:    params.Tmux,
				Session: agents.ManagerSession,
			}),
		},
		agentGit: agentgit.NewGitService(agentgit.NewGitServiceParams{
			Agents: st.agents,
			Git:    gitClient,
		}),
		watchRows: watch.NewRows(buildWatchLoaders(st)),
	}, nil
}

type buildScriptsParams struct {
	Server     NewServerParams
	Stores     *stores
	LoadConfig func() (*config.Config, error)
	RunLogsDir string
}

func buildScripts(params buildScriptsParams) (*scriptrun.ScriptService, error) {
	server, st := params.Server, params.Stores

	builtinScriptsDir := BuiltinScriptsDir(server.Env.CacheDir)
	if err := scripts.InstallBuiltins(builtinScriptsDir); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(params.RunLogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", params.RunLogsDir, err)
	}

	return scriptrun.NewScriptService(scriptrun.NewScriptServiceParams{
		Agents:     st.agents,
		Workflows:  st.workflows,
		Tasks:      st.tasks,
		Tmux:       server.Tmux,
		Runner:     scripts.NewRunner(),
		Runs:       st.runs,
		Load:       params.LoadConfig,
		Addr:       server.Addr,
		Socket:     server.Env.TmuxSocket,
		BuiltinDir: builtinScriptsDir,
		LogsDir:    params.RunLogsDir,
		Editor:     server.Env.Editor,
		BinPath:    server.HookCommand,
	}), nil
}

type buildAgentsParams struct {
	Server     NewServerParams
	Stores     *stores
	Wakers     *loopWakers
	Git        *git.Client
	Queue      *jobs.Enqueuer
	Scripts    *scriptrun.ScriptService
	Panes      *pane.Pane
	Settler    *agents.Settler
	Tasks      *tasks.TaskService
	RunLogsDir string
	LoadConfig func() (*config.Config, error)
}

func buildAgents(params buildAgentsParams) agentServices {
	server, st := params.Server, params.Stores

	harnessLauncher := agents.NewLauncher(agents.NewLauncherParams{
		Tmux:            server.Tmux,
		Addr:            server.Addr,
		CacheDir:        server.Env.CacheDir,
		LogPath:         server.Env.LogPath(),
		ContextPath:     server.ContextPath,
		BinPath:         server.HookCommand,
		Usage:           server.Usage,
		TrustLaunchDirs: server.TrustDirs,
	})
	launchService := agents.NewLaunchService(agents.NewLaunchServiceParams{
		Tmux:               server.Tmux,
		Agents:             st.agents,
		Reviews:            st.review,
		Workflows:          st.workflows,
		Tx:                 st.pool,
		Launcher:           harnessLauncher,
		Tasks:              params.Tasks,
		Harnesses:          server.Harnesses,
		Scripts:            params.Scripts,
		UserDefaultHarness: server.DefaultHarness,
		UserDefaultFlags:   server.DefaultFlags,
		Usage:              server.Usage,
	})
	resumeService := agents.NewResumeService(agents.NewResumeServiceParams{
		Tmux:               server.Tmux,
		Agents:             st.agents,
		Workflows:          st.workflows,
		Launcher:           harnessLauncher,
		Harnesses:          server.Harnesses,
		Scripts:            params.Scripts,
		FallbackResumeRoot: server.FallbackResumeRoot,
	})
	agentDelete := jobs.NewAgentDelete(params.Queue)
	emptyTasks := agents.NewEmptyTaskDeleter(agents.NewEmptyTaskDeleterParams{
		Tasks:     st.tasks,
		Workflows: st.workflows,
		Scripts:   params.Scripts,
	})
	emptyWorkflows := agents.NewEmptyWorkflowDeleter(agents.NewEmptyWorkflowDeleterParams{
		Workflows: st.workflows,
		Agents:    st.agents,
		Schedules: st.schedules,
		Tasks:     emptyTasks,
	})
	screenKeeper := agents.NewScreenKeeper(agents.NewScreenKeeperParams{
		Panes:   server.Tmux,
		Screens: st.screens,
	})
	lifecycleService := agents.NewLifecycleService(agents.NewLifecycleServiceParams{
		Tmux:           server.Tmux,
		Agents:         st.agents,
		Workflows:      st.workflows,
		EmptyWorkflows: emptyWorkflows,
		EmptyTasks:     emptyTasks,
		AgentDeleter:   agentDelete,
		Tx:             st.pool,
		Scripts:        params.Scripts,
		Reviews:        st.review,
		Snapshots:      params.Git,
		Screens:        screenKeeper,
		LogsDir:        params.RunLogsDir,
		FramesDir:      server.Env.FramesDir(),
		Usage:          server.Usage,
		Tasks:          params.Tasks,
	})
	reconciler := agents.NewReconciler(agents.NewReconcilerParams{
		Panes:    server.Tmux,
		Observer: params.Panes,
		States:   st.agents,
	}).WithFrameRecorder(frames.NewRecorder(server.Env.FramesDir(), newFrameSettings(params.LoadConfig).get)).WithScreenKeeper(screenKeeper)

	return agentServices{
		liveness:  agents.NewPaneLiveness(server.Tmux),
		screens:   screenKeeper,
		launch:    launchService,
		resume:    resumeService,
		lifecycle: lifecycleService,
		adopt: agents.NewAdoptService(agents.NewAdoptServiceParams{
			Agents:    st.agents,
			Workflows: st.workflows,
			Launch:    launchService,
			Worktrees: params.Git,
			Tasks:     params.Tasks,
		}),
		workflow: agents.NewWorkflowService(agents.NewWorkflowServiceParams{
			Workflows:      st.workflows,
			Agents:         st.agents,
			AgentDeleter:   agentDelete,
			EmptyWorkflows: emptyWorkflows,
			Schedules:      st.schedules,
			Lifecycle:      lifecycleService,
			Tx:             st.pool,
			Hooks:          params.Scripts,
		}),
		orchestrator: agents.NewOrchestratorService(agents.NewOrchestratorServiceParams{
			Agents:      st.agents,
			Workflows:   st.workflows,
			Transcripts: st.transcripts,
			Harnesses:   server.Harnesses,
			Launch:      launchService,
			Lifecycle:   lifecycleService,
			Resume:      resumeService,
			StateDir:    server.Env.StateDir,
		}),
		holder: agents.NewHolder(agents.NewHolderParams{
			Agents:  st.agents,
			Channel: params.Panes,
			Settler: params.Settler,
		}),
		reconciler: reconciler,
		rowReader: agents.NewRowReader(agents.NewRowReaderParams{
			Agents:        st.agents,
			Rows:          st.rows,
			Mail:          st.mail,
			PullRequests:  st.pullRequests,
			Repositories:  st.pullRequestSyncs,
			Reviews:       st.review,
			ReviewThreads: st.reviewThreads,
			Workflows:     st.workflows,
			Tasks:         st.tasks,
		}),
		rowWriter: agents.NewRowWriter(agents.NewRowWriterParams{
			Agents:         st.agents,
			Runs:           st.runs,
			Reconciler:     reconciler,
			CwdSync:        params.Wakers.cwdSync,
			TranscriptSync: params.Wakers.transcriptSync,
		}),
		delete: agentDelete,
	}
}

type buildPullRequestsParams struct {
	Server     NewServerParams
	Stores     *stores
	Queue      *jobs.Enqueuer
	Git        *git.Client
	Wakers     *loopWakers
	LoadConfig func() (*config.Config, error)
}

func buildPullRequests(params buildPullRequestsParams) pullRequestServices {
	server, st := params.Server, params.Stores

	builders := server.ForgeBuilders
	if builders == nil {
		builders = forges.Builders
	}
	tokenCachePath := forges.TokenCachePath(server.Env.CacheDir)
	forgeService := forge.NewService(forge.NewServiceParams{
		Connections: st.forges,
		Credentials: forge.NewFileStore(server.Env.StateDir),
		Builders:    builders,
		LoadApp: func() (forge.App, error) {
			cfg, err := params.LoadConfig()
			if err != nil {
				return forge.App{}, err
			}

			return forges.AppFrom(cfg.GitHubApp, tokenCachePath)
		},
	})
	pullRequestSyncer := pullrequests.NewPullRequestSyncer(pullrequests.NewPullRequestSyncerParams{
		Forges:       forgeService,
		PullRequests: st.pullRequests,
		Syncs:        st.pullRequestSyncs,
		Events:       params.Queue,
		Loop:         params.Wakers.pullRequestSync,
	})

	return pullRequestServices{
		forge: forgeService,
		sync:  pullRequestSyncer,
		forgeReply: reviews.NewForgeReplyService(reviews.NewForgeReplyServiceParams{
			Reviews: st.review,
			Forges:  forgeService,
		}),
		commentSync: pullrequests.NewCommentSyncer(pullrequests.NewCommentSyncerParams{
			Forges:  forgeService,
			Threads: st.forgeThreads,
			Tx:      st.pool,
			Mirror: reviews.NewForgeMirror(reviews.NewForgeMirrorParams{
				Reviews:      st.review,
				ForgeThreads: st.forgeThreads,
				Git:          params.Git,
				Events:       params.Queue,
				BotNames: func() ([]string, error) {
					cfg, err := params.LoadConfig()
					if err != nil {
						return nil, err
					}

					return cfg.Forge.BotNames, nil
				},
			}),
		}),
		lister: pullrequests.NewLister(pullrequests.NewListerParams{PullRequests: st.pullRequests, StatusChecks: st.statusChecks}),
		writer: pullrequests.NewWriter(pullrequests.NewWriterParams{
			Forges: forgeService,
			Sync:   pullRequestSyncer,
		}),
	}
}

func buildWorktrees(st *stores, gitClient *git.Client, loadConfig func() (*config.Config, error)) worktreeServices {
	lister := worktrees.NewLister(worktrees.NewListerParams{
		SharedDir: func() (string, error) {
			cfg, err := loadConfig()
			if err != nil {
				return "", err
			}

			return cfg.GetWorktreesDir()
		},
		Agents:    st.agents,
		Tasks:     st.tasks,
		Workflows: st.workflows,
		Worktrees: st.worktrees,
	})

	checkouts := worktrees.NewCheckoutReader(worktrees.NewCheckoutReaderParams{Git: gitClient})

	return worktreeServices{
		lister:    lister,
		checkouts: checkouts,
		inspector: worktrees.NewInspector(worktrees.NewInspectorParams{
			Lister:    lister,
			Git:       gitClient,
			Checkouts: checkouts,
			Workflows: st.workflows,
			Worktrees: st.worktrees,
		}),
		remover: worktrees.NewRemover(worktrees.NewRemoverParams{
			Lister: lister,
			Agents: st.agents,
			Git:    gitClient,
		}),
	}
}

// getMaxCheckLogBytes is the largest check log komrad keeps, read from the config each time.
func (s *services) getMaxCheckLogBytes() (int64, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return 0, err
	}

	return cfg.Forge.GetMaxCheckLogBytes()
}
