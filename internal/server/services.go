package server

import (
	"fmt"
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/browser"
	"weave/internal/frames"
	"weave/internal/git"
	"weave/internal/harness/pane"
	"weave/internal/scripts"
)

type services struct {
	loadConfig   func() (*config.Config, error)
	script       *agents.ScriptService
	pullRequests *agents.PullRequestOpener
	mail         *agents.MailService
	review       *agents.ReviewService
	schedule     *agents.ScheduleService
	liveness     *agents.PaneLiveness
	launch       *agents.LaunchService
	resume       *agents.ResumeService
	adopt        *agents.AdoptService
	lifecycle    *agents.LifecycleService
	orchestrator *agents.OrchestratorService
	workflow     *agents.WorkflowService
	delivery     *agents.DeliveryService
	reconciler   *agents.Reconciler
	marks        *agents.MarkService
	holder       *agents.Holder
	rebase       *agents.RebaseService
}

// BuiltinScriptsDir is where the scripts weave ships are installed under a cache directory,
// which is the only place a script that is not a hook is resolved from.
func BuiltinScriptsDir(cacheDir string) string {
	return filepath.Join(cacheDir, "weave-builtin-scripts")
}

func buildServices(params NewServerParams, st *stores) (*services, error) {
	loadConfig := func() (*config.Config, error) { return config.Load(params.ConfigPaths) }

	mailService := agents.NewMailService(agents.NewMailServiceParams{
		MailReadWriter: st.mail,
		Agents:         st.agents,
		Config:         mailConfig(loadConfig),
	})
	stateDir, err := config.GetStateDir()
	if err != nil {
		return nil, err
	}

	scheduleService := agents.NewScheduleService(agents.NewScheduleServiceParams{
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

	scriptService := agents.NewScriptService(agents.NewScriptServiceParams{
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

	opener := params.Opener
	if opener == nil {
		opener = browser.NewDesktop()
	}

	pullRequestOpener := agents.NewPullRequestOpener(agents.NewPullRequestOpenerParams{
		Agents: st.agents,
		Opener: opener,
	})
	harnessLauncher := agents.NewLauncher(agents.NewLauncherParams{
		Tmux:        params.Tmux,
		Addr:        params.Addr,
		CacheDir:    params.Env.CacheDir,
		LogPath:     params.Env.LogPath(),
		ContextPath: params.ContextPath,
		HookCommand: params.HookCommand,
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
	reviewService := agents.NewReviewService(agents.NewReviewServiceParams{
		Threads:   st.review,
		Messages:  st.review,
		Viewed:    st.review,
		Rounds:    st.review,
		Reviews:   st.review,
		Snapshots: git.NewClient(),
		Workflows: st.workflows,
		Agents:    st.agents,
		Mailer:    mailService,
		Resumer:   resumeService,
		StateDir:  stateDir,
		Interrupter: agents.NewInterrupter(agents.NewInterrupterParams{
			Agents:  st.agents,
			Channel: paneChannel,
		}),
	})
	rebaseService := agents.NewRebaseService(agents.NewRebaseServiceParams{
		Agents:    st.agents,
		Workflows: st.workflows,
		Notifier:  mailService,
		Rebaser:   git.NewClient(),
		Interrupter: agents.NewInterrupter(agents.NewInterrupterParams{
			Agents:  st.agents,
			Channel: paneChannel,
		}),
	})
	adoptService := agents.NewAdoptService(agents.NewAdoptServiceParams{
		Agents:       st.agents,
		Workflows:    st.workflows,
		Launch:       launchService,
		PullRequests: agents.NewGHPullRequests(git.NewClient()),
		Worktrees:    git.NewClient(),
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
		Snapshots:      git.NewClient(),
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

	markService := agents.NewMarkService(agents.NewMarkServiceParams{Marks: st.marks, Tx: st.pool})

	return &services{
		loadConfig:   loadConfig,
		marks:        markService,
		rebase:       rebaseService,
		script:       scriptService,
		pullRequests: pullRequestOpener,
		mail:         mailService,
		review:       reviewService,
		schedule:     scheduleService,
		liveness:     paneLiveness,
		launch:       launchService,
		resume:       resumeService,
		holder: agents.NewHolder(agents.NewHolderParams{
			Agents:   st.agents,
			Channel:  paneChannel,
			Observer: paneChannel,
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
		delivery: agents.NewDeliveryService(agents.NewDeliveryServiceParams{
			MailboxReader: st.mail,
			NudgeClaimer:  st.agents,
			PaneReader:    paneLiveness,
			Channel:       paneChannel,
		}),
		reconciler: agents.NewReconciler(agents.NewReconcilerParams{
			Panes:    params.Tmux,
			Observer: paneChannel,
			States:   st.agents,
		}).
			WithPRFinder(agents.NewGHPRFinder()).
			WithFrameRecorder(frames.NewRecorder(params.Env.FramesDir(), newFrameSettings(loadConfig).get)),
	}, nil
}
