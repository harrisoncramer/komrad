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
	"komrad/internal/transcripts"
	"komrad/internal/watch"
	"komrad/internal/worktrees"
)

type services struct {
	loadConfig       func() (*config.Config, error)
	script           *scriptrun.ScriptService
	mail             *mail.MailService
	review           *reviews.ReviewService
	schedule         *schedules.ScheduleService
	liveness         *agents.PaneLiveness
	rowWriter        *agents.RowWriter
	rowReader        *agents.RowReader
	transcriptReader *transcripts.TranscriptReader
	launch           *agents.LaunchService
	resume           *agents.ResumeService
	adopt            *agents.AdoptService
	lifecycle        *agents.LifecycleService
	agentDelete      *jobs.AgentDelete
	queue            *jobs.Enqueuer
	orchestrator     *agents.OrchestratorService
	workflow         *agents.WorkflowService
	delivery         *mail.DeliveryService
	reconciler       *agents.Reconciler
	documents        *documents.DocumentService
	holder           *agents.Holder
	checkout         *checkouts.Service
	cwdSync          *checkouts.CwdSyncer
	transcriptSync   *transcripts.TranscriptSyncer
	forge            *forge.Service
	pullRequestSync  *pullrequests.PullRequestSyncer
	commentSync      *pullrequests.CommentSyncer
	pullRequests     *pullrequests.Lister
	writer           *pullrequests.Writer
	dismisser        *pullrequests.Dismisser
	worktreeLister   *worktrees.Lister
	inspector        *worktrees.Inspector
	remover          *worktrees.Remover
	watchRows        *watch.Rows
	shells           *shells.ShellService
	attaches         *attaches.AttachService
	agentGit         *agentgit.GitService
	screens          *agents.ScreenKeeper
}

// BuiltinScriptsDir is where the scripts komrad ships are installed under a cache directory,
// which is the only place a script that is not a hook is resolved from.
func BuiltinScriptsDir(cacheDir string) string {
	return filepath.Join(cacheDir, "komrad-builtin-scripts")
}

func buildServices(params NewServerParams, st *stores, wakers *loopWakers) (*services, error) {
	loadConfig := func() (*config.Config, error) { return config.LoadStack(params.ConfigStack) }
	gitClient := git.NewClient()

	mailService := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: st.mail,
		Agents:         st.agents,
		Workflows:      st.workflows,
		Config:         mailConfig(loadConfig),
	})
	stateDir := params.Env.StateDir
	scheduleService := schedules.NewScheduleService(schedules.NewScheduleServiceParams{
		Schedules: st.schedules,
		Workflows: st.workflows,
		Agents:    st.agents,
	})

	builtinScriptsDir := BuiltinScriptsDir(params.Env.CacheDir)
	if err := scripts.InstallBuiltins(builtinScriptsDir); err != nil {
		return nil, err
	}

	runLogsDir := params.Env.RunLogsDir()
	if err := os.MkdirAll(runLogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", runLogsDir, err)
	}

	scriptService := scriptrun.NewScriptService(scriptrun.NewScriptServiceParams{
		Agents:     st.agents,
		Workflows:  st.workflows,
		Tmux:       params.Tmux,
		Runner:     scripts.NewRunner(),
		Runs:       st.runs,
		Load:       loadConfig,
		Addr:       params.Addr,
		Socket:     params.Env.TmuxSocket,
		BuiltinDir: builtinScriptsDir,
		LogsDir:    runLogsDir,
		Editor:     params.Env.Editor,
		BinPath:    params.HookCommand,
	})

	harnessLauncher := agents.NewLauncher(agents.NewLauncherParams{
		Tmux:            params.Tmux,
		Addr:            params.Addr,
		CacheDir:        params.Env.CacheDir,
		LogPath:         params.Env.LogPath(),
		ContextPath:     params.ContextPath,
		BinPath:         params.HookCommand,
		Usage:           params.Usage,
		TrustLaunchDirs: params.TrustDirs,
	})
	paneLiveness := agents.NewPaneLiveness(params.Tmux)
	paneChannel := pane.NewPane(pane.NewPaneParams{Terminal: params.Tmux, Harnesses: params.Harnesses})
	launchService := agents.NewLaunchService(agents.NewLaunchServiceParams{
		Tmux:               params.Tmux,
		Agents:             st.agents,
		Reviews:            st.review,
		Workflows:          st.workflows,
		Tx:                 st.pool,
		Launcher:           harnessLauncher,
		Harnesses:          params.Harnesses,
		Scripts:            scriptService,
		UserDefaultHarness: params.DefaultHarness,
		UserDefaultFlags:   params.DefaultFlags,
		Usage:              params.Usage,
	})
	resumeService := agents.NewResumeService(agents.NewResumeServiceParams{
		Tmux:               params.Tmux,
		Agents:             st.agents,
		Workflows:          st.workflows,
		Launcher:           harnessLauncher,
		Harnesses:          params.Harnesses,
		Scripts:            scriptService,
		FallbackResumeRoot: params.FallbackResumeRoot,
	})
	inserter, err := jobs.NewInsertOnlyClient(st.pool.GetRawPool(), st.pool.GetRiverSchema())
	if err != nil {
		return nil, err
	}
	queue := jobs.NewEnqueuer(inserter, st.pool.GetRawPool())
	reviewService := reviews.NewReviewService(reviews.NewReviewServiceParams{
		Threads:   st.review,
		Messages:  st.review,
		Viewed:    st.review,
		Rounds:    st.review,
		Reviews:   st.review,
		Snapshots: gitClient,
		Workflows: st.workflows,
		Agents:    st.agents,
		Events:    queue,
		StateDir:  stateDir,
		Interrupter: agents.NewInterrupter(agents.NewInterrupterParams{
			Agents:   st.agents,
			Channel:  paneChannel,
			Observer: paneChannel,
		}),
	})
	settler := agents.NewSettler(agents.NewSettlerParams{
		Channel:  paneChannel,
		Observer: paneChannel,
	})
	checkoutService := checkouts.NewService(checkouts.NewServiceParams{
		Agents:          st.agents,
		Workflows:       st.workflows,
		Settler:         settler,
		Events:          queue,
		Rebaser:         gitClient,
		Pusher:          gitClient,
		Aborter:         gitClient,
		PullRequests:    st.pullRequests,
		CheckoutSync:    wakers.cwdSync,
		PullRequestSync: wakers.pullRequestSync,
	})
	adoptService := agents.NewAdoptService(agents.NewAdoptServiceParams{
		Agents:          st.agents,
		Workflows:       st.workflows,
		Launch:          launchService,
		Worktrees:       gitClient,
		PullRequestSync: wakers.pullRequestSync,
	})
	agentDelete := jobs.NewAgentDelete(queue)

	emptyWorkflows := agents.NewEmptyWorkflowDeleter(agents.NewEmptyWorkflowDeleterParams{
		Workflows: st.workflows,
		Agents:    st.agents,
		Schedules: st.schedules,
	})
	screenKeeper := agents.NewScreenKeeper(agents.NewScreenKeeperParams{
		Panes:   params.Tmux,
		Screens: st.screens,
	})
	lifecycleService := agents.NewLifecycleService(agents.NewLifecycleServiceParams{
		Tmux:           params.Tmux,
		Agents:         st.agents,
		Workflows:      st.workflows,
		EmptyWorkflows: emptyWorkflows,
		Tx:             st.pool,
		Scripts:        scriptService,
		Reviews:        st.review,
		Snapshots:      gitClient,
		Screens:        screenKeeper,
		LogsDir:        runLogsDir,
		FramesDir:      params.Env.FramesDir(),
		Usage:          params.Usage,
	})
	workflowService := agents.NewWorkflowService(agents.NewWorkflowServiceParams{
		Workflows:    st.workflows,
		Pages:        st.workflows,
		Agents:       st.agents,
		AgentDeleter: agentDelete,
		Emptied:      emptyWorkflows,
		Schedules:    st.schedules,
		Tmux:         params.Tmux,
		Screens:      screenKeeper,
		Tx:           st.pool,
		Scripts:      scriptService,
		Hooks:        scriptService,
		Usage:        params.Usage,
	})

	builders := params.ForgeBuilders
	if builders == nil {
		builders = forges.Builders
	}
	tokenCachePath := forges.TokenCachePath(params.Env.CacheDir)
	forgeService := forge.NewService(forge.NewServiceParams{
		Connections: st.forges,
		Credentials: forge.NewFileStore(stateDir),
		Builders:    builders,
		LoadApp: func() (forge.App, error) {
			cfg, err := loadConfig()
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
		Events:       queue,
		Recheck:      wakers.pullRequestSync,
	})
	cwdSyncer := checkouts.NewCwdSyncer(checkouts.NewCwdSyncerParams{
		Agents:       st.agents,
		Heads:        gitClient,
		Trees:        gitClient,
		Rounds:       st.review,
		Workflows:    st.workflows,
		PullRequests: st.pullRequests,
	})

	transcriptSyncer := transcripts.NewTranscriptSyncer(transcripts.NewTranscriptSyncerParams{
		Agents:      st.agents,
		Transcripts: st.transcripts,
		Harnesses:   params.Harnesses,
	})

	worktreeLister := worktrees.NewLister(worktrees.NewListerParams{
		SharedDir: func() (string, error) {
			cfg, err := loadConfig()
			if err != nil {
				return "", err
			}

			return cfg.GetWorktreesDir()
		},
		Agents:    st.agents,
		Workflows: st.workflows,
		Sizes:     st.worktrees,
	})

	reconciler := agents.NewReconciler(agents.NewReconcilerParams{
		Panes:    params.Tmux,
		Observer: paneChannel,
		States:   st.agents,
	}).WithFrameRecorder(frames.NewRecorder(params.Env.FramesDir(), newFrameSettings(loadConfig).get)).WithScreenKeeper(screenKeeper)

	return &services{
		loadConfig: loadConfig,
		screens:    screenKeeper,
		watchRows:  watch.NewRows(buildWatchLoaders(st)),
		transcriptReader: transcripts.NewTranscriptReader(transcripts.NewTranscriptReaderParams{
			Agents:      st.agents,
			Transcripts: st.transcripts,
		}),
		rowReader: agents.NewRowReader(agents.NewRowReaderParams{
			Agents:       st.agents,
			Rows:         st.rows,
			Mail:         st.mail,
			PullRequests: st.pullRequests,
			Repositories: st.pullRequestSyncs,
			Reviews:      st.review,
			Workflows:    st.workflows,
		}),
		rowWriter: agents.NewRowWriter(agents.NewRowWriterParams{
			Agents:         st.agents,
			Runs:           st.runs,
			Reconciler:     reconciler,
			CwdSync:        wakers.cwdSync,
			TranscriptSync: wakers.transcriptSync,
		}),
		documents: documents.NewDocumentService(documents.NewDocumentServiceParams{
			Agents:    st.agents,
			Documents: st.documents,
			Types:     st.documentTypes,
			Events:    queue,
		}),
		checkout:        checkoutService,
		cwdSync:         cwdSyncer,
		transcriptSync:  transcriptSyncer,
		forge:           forgeService,
		pullRequestSync: pullRequestSyncer,
		commentSync: pullrequests.NewCommentSyncer(pullrequests.NewCommentSyncerParams{
			Forges:  forgeService,
			Threads: st.forgeThreads,
			Tx:      st.pool,
		}),
		pullRequests: pullrequests.NewLister(pullrequests.NewListerParams{PullRequests: st.pullRequests}),
		dismisser:    pullrequests.NewDismisser(pullrequests.NewDismisserParams{Threads: st.forgeThreads}),
		writer: pullrequests.NewWriter(pullrequests.NewWriterParams{
			Forges: forgeService,
			Sync:   wakers.pullRequestSync,
		}),
		worktreeLister: worktreeLister,
		shells: shells.NewShellService(shells.NewShellServiceParams{
			Agents: st.agents,
			Shell:  params.Env.Shell,
		}),
		attaches: attaches.NewAttachService(attaches.NewAttachServiceParams{
			Agents:  st.agents,
			Tmux:    params.Tmux,
			Session: agents.ManagerSession,
		}),
		agentGit: agentgit.NewGitService(agentgit.NewGitServiceParams{
			Agents: st.agents,
			Git:    gitClient,
		}),
		inspector: worktrees.NewInspector(worktrees.NewInspectorParams{
			Lister:    worktreeLister,
			Git:       gitClient,
			Workflows: st.workflows,
			Worktrees: st.worktrees,
		}),
		remover: worktrees.NewRemover(worktrees.NewRemoverParams{
			Lister: worktreeLister,
			Agents: st.agents,
			Git:    gitClient,
		}),
		script:   scriptService,
		mail:     mailService,
		review:   reviewService,
		schedule: scheduleService,
		liveness: paneLiveness,
		launch:   launchService,
		resume:   resumeService,
		holder: agents.NewHolder(agents.NewHolderParams{
			Agents:  st.agents,
			Channel: paneChannel,
			Settler: settler,
		}),
		adopt:       adoptService,
		lifecycle:   lifecycleService,
		agentDelete: agentDelete,
		queue:       queue,
		orchestrator: agents.NewOrchestratorService(agents.NewOrchestratorServiceParams{
			Agents:      st.agents,
			Workflows:   st.workflows,
			Transcripts: st.transcripts,
			Harnesses:   params.Harnesses,
			Launch:      launchService,
			Lifecycle:   lifecycleService,
			Resume:      resumeService,
			StateDir:    stateDir,
		}),
		workflow: workflowService,
		delivery: mail.NewDeliveryService(mail.NewDeliveryServiceParams{
			MailboxReader: st.mail,
			NudgeClaimer:  st.agents,
			PaneReader:    paneLiveness,
			Channel:       paneChannel,
		}),
		reconciler: reconciler,
	}, nil
}
