package server

import (
	"fmt"
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/checkouts"
	"weave/internal/forge"
	"weave/internal/forge/forges"
	"weave/internal/frames"
	"weave/internal/git"
	"weave/internal/harness/pane"
	"weave/internal/jobs"
	"weave/internal/mail"
	"weave/internal/marks"
	"weave/internal/pullrequests"
	"weave/internal/reviews"
	"weave/internal/schedules"
	"weave/internal/scriptrun"
	"weave/internal/scripts"
	"weave/internal/transcripts"
	"weave/internal/watch"
	"weave/internal/worktrees"
)

type services struct {
	loadConfig      func() (*config.Config, error)
	script          *scriptrun.ScriptService
	mail            *mail.MailService
	review          *reviews.ReviewService
	schedule        *schedules.ScheduleService
	liveness        *agents.PaneLiveness
	rowWriter       *agents.RowWriter
	launch          *agents.LaunchService
	resume          *agents.ResumeService
	adopt           *agents.AdoptService
	lifecycle       *agents.LifecycleService
	orchestrator    *agents.OrchestratorService
	workflow        *agents.WorkflowService
	delivery        *mail.DeliveryService
	reconciler      *agents.Reconciler
	marks           *marks.MarkService
	holder          *agents.Holder
	rebase          *checkouts.RebaseService
	cwdSync         *checkouts.CwdSyncer
	transcriptSync  *transcripts.TranscriptSyncer
	forge           *forge.Service
	pullRequestSync *pullrequests.PullRequestSyncer
	worktreeLister  *worktrees.Lister
	inspector       *worktrees.Inspector
	remover         *worktrees.Remover
	measurer        *worktrees.Measurer
	watchRows       *watch.Rows
	runs            *jobs.RunLister
}

// BuiltinScriptsDir is where the scripts weave ships are installed under a cache directory,
// which is the only place a script that is not a hook is resolved from.
func BuiltinScriptsDir(cacheDir string) string {
	return filepath.Join(cacheDir, "weave-builtin-scripts")
}

func buildServices(params NewServerParams, st *stores) (*services, error) {
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
		Tmux:        params.Tmux,
		Addr:        params.Addr,
		CacheDir:    params.Env.CacheDir,
		LogPath:     params.Env.LogPath(),
		ContextPath: params.ContextPath,
		BinPath:     params.HookCommand,
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
		MaxAgents:          params.MaxAgents,
		MaxChildren:        params.MaxChildren,
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
	reviewService := reviews.NewReviewService(reviews.NewReviewServiceParams{
		Threads:   st.review,
		Messages:  st.review,
		Viewed:    st.review,
		Rounds:    st.review,
		Reviews:   st.review,
		Snapshots: gitClient,
		Workflows: st.workflows,
		Agents:    st.agents,
		Mailer:    mailService,
		Resumer:   resumeService,
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
	rebaseService := checkouts.NewRebaseService(checkouts.NewRebaseServiceParams{
		Agents:    st.agents,
		Workflows: st.workflows,
		Settler:   settler,
		Notifier:  mailService,
		Rebaser:   gitClient,
	})
	adoptService := agents.NewAdoptService(agents.NewAdoptServiceParams{
		Agents:    st.agents,
		Workflows: st.workflows,
		Launch:    launchService,
		Worktrees: gitClient,
	})
	emptyWorkflows := agents.NewEmptyWorkflowDeleter(agents.NewEmptyWorkflowDeleterParams{
		Workflows: st.workflows,
		Agents:    st.agents,
		Schedules: st.schedules,
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
		LogsDir:        runLogsDir,
		FramesDir:      params.Env.FramesDir(),
		MaxAgents:      params.MaxAgents,
		MaxChildren:    params.MaxChildren,
	})
	workflowService := agents.NewWorkflowService(agents.NewWorkflowServiceParams{
		Workflows:    st.workflows,
		Agents:       st.agents,
		AgentDeleter: lifecycleService,
		Emptied:      emptyWorkflows,
		Schedules:    st.schedules,
		Tmux:         params.Tmux,
		Tx:           st.pool,
		Scripts:      scriptService,
		MaxAgents:    params.MaxAgents,
		MaxChildren:  params.MaxChildren,
	})

	markService := marks.NewMarkService(marks.NewMarkServiceParams{Marks: st.marks, Tx: st.pool})

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
		Tx:           st.pool,
	})
	cwdSyncer := checkouts.NewCwdSyncer(checkouts.NewCwdSyncerParams{
		Agents:  st.agents,
		Heads:   gitClient,
		Trees:   gitClient,
		Tallies: st.review,
		Bases:   st.workflows,
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
		Sizes:     st.sizes,
	})

	reconciler := agents.NewReconciler(agents.NewReconcilerParams{
		Panes:    params.Tmux,
		Observer: paneChannel,
		States:   st.agents,
	}).WithFrameRecorder(frames.NewRecorder(params.Env.FramesDir(), newFrameSettings(loadConfig).get))

	runs, err := jobs.NewRunLister(st.pool.GetRawPool(), st.pool.GetRiverSchema())
	if err != nil {
		return nil, err
	}

	return &services{
		loadConfig: loadConfig,
		runs:       runs,
		watchRows:  watch.NewRows(buildWatchLoaders(st, runs)),
		rowWriter: agents.NewRowWriter(agents.NewRowWriterParams{
			Agents:     st.agents,
			Runs:       st.runs,
			Reconciler: reconciler,
		}),
		marks:           markService,
		rebase:          rebaseService,
		cwdSync:         cwdSyncer,
		transcriptSync:  transcriptSyncer,
		forge:           forgeService,
		pullRequestSync: pullRequestSyncer,
		worktreeLister:  worktreeLister,
		inspector: worktrees.NewInspector(worktrees.NewInspectorParams{
			Lister:    worktreeLister,
			Git:       gitClient,
			Workflows: st.workflows,
			Sizes:     st.sizes,
		}),
		remover: worktrees.NewRemover(worktrees.NewRemoverParams{
			Lister: worktreeLister,
			Agents: st.agents,
			Git:    gitClient,
		}),
		measurer: worktrees.NewMeasurer(worktrees.NewMeasurerParams{
			Lister: worktreeLister,
			Sizes:  st.sizes,
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
		adopt:     adoptService,
		lifecycle: lifecycleService,
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
