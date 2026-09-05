package server

import (
	"fmt"
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/browser"
	"weave/internal/frames"
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
	workflow     *agents.WorkflowService
	delivery     *agents.DeliveryService
	reconciler   *agents.Reconciler
}

// BuiltinScriptsDir is where the scripts weave ships are installed under a cache directory,
// which is the only place a script that is not a hook is resolved from.
func BuiltinScriptsDir(cacheDir string) string {
	return filepath.Join(cacheDir, "weave-builtin-scripts")
}

func buildServices(p Params, st *stores) (*services, error) {
	loadConfig := func() (*config.Config, error) { return config.Load(p.ConfigPaths) }

	mailService := agents.NewMailService(agents.NewMailServiceParams{
		MailReadWriter: st.mail,
		Agents:         st.agents,
		Config:         mailConfig(loadConfig),
	})
	reviewService := agents.NewReviewService(agents.ReviewServiceParams{
		Threads:  st.review,
		Messages: st.review,
		Viewed:   st.review,
		Agents:   st.agents,
		Mailer:   mailService,
		Interrupter: agents.NewPaneInterrupter(agents.PaneInterrupterParams{
			Agents:    st.agents,
			Harnesses: p.Harnesses,
			Panes:     p.Tmux,
		}),
	})
	scheduleService := agents.NewScheduleService(agents.ScheduleParams{
		Schedules: st.schedules,
		Workflows: st.workflows,
	})

	builtinScriptsDir := BuiltinScriptsDir(p.Env.CacheDir)
	if err := scripts.InstallBuiltins(builtinScriptsDir); err != nil {
		return nil, err
	}

	runLogsDir := p.Env.RunLogsDir()
	if err := os.MkdirAll(runLogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", runLogsDir, err)
	}

	scriptService := agents.NewScriptService(agents.ScriptParams{
		Agents:     st.agents,
		Workflows:  st.workflows,
		Tmux:       p.Tmux,
		Runner:     scripts.NewRunner(),
		Runs:       st.runs,
		Load:       loadConfig,
		Addr:       p.Addr,
		Socket:     p.Env.TmuxSocket,
		BuiltinDir: builtinScriptsDir,
		LogsDir:    runLogsDir,
		Editor:     p.Env.Editor,
		BinPath:    p.HookCommand,
	})

	opener := p.Opener
	if opener == nil {
		opener = browser.NewDesktop()
	}

	pullRequestOpener := agents.NewPullRequestOpener(agents.PullRequestOpenerParams{
		Agents: st.agents,
		Opener: opener,
	})
	harnessLauncher := agents.NewLauncher(agents.LauncherParams{
		Tmux:        p.Tmux,
		Addr:        p.Addr,
		CacheDir:    p.Env.CacheDir,
		LogPath:     p.Env.LogPath(),
		ContextPath: p.ContextPath,
		HookCommand: p.HookCommand,
	})
	paneLiveness := agents.NewPaneLiveness(p.Tmux)
	launchService := agents.NewLaunchService(agents.LaunchServiceParams{
		Tmux:               p.Tmux,
		Agents:             st.agents,
		Workflows:          st.workflows,
		Tx:                 st.pool,
		Launcher:           harnessLauncher,
		Harnesses:          p.Harnesses,
		Scripts:            scriptService,
		UserDefaultHarness: p.DefaultHarness,
		UserDefaultFlags:   p.DefaultFlags,
		MaxAgents:          p.MaxAgents,
		MaxChildren:        p.MaxChildren,
	})
	resumeService := agents.NewResumeService(agents.ResumeServiceParams{
		Tmux:               p.Tmux,
		Agents:             st.agents,
		Workflows:          st.workflows,
		Launcher:           harnessLauncher,
		Harnesses:          p.Harnesses,
		Scripts:            scriptService,
		FallbackResumeRoot: p.FallbackResumeRoot,
	})
	adoptService := agents.NewAdoptService(agents.AdoptServiceParams{
		Agents:       st.agents,
		Workflows:    st.workflows,
		Launch:       launchService,
		PullRequests: agents.NewGHPullRequests(),
	})
	emptyWorkflows := agents.NewEmptyWorkflowDeleter(agents.EmptyWorkflowDeleterParams{
		Workflows: st.workflows,
		Agents:    st.agents,
		Schedules: st.schedules,
	})
	lifecycleService := agents.NewLifecycleService(agents.LifecycleServiceParams{
		Tmux:           p.Tmux,
		Agents:         st.agents,
		Workflows:      st.workflows,
		EmptyWorkflows: emptyWorkflows,
		Tx:             st.pool,
		Scripts:        scriptService,
		Notifier:       mailService,
		LogsDir:        runLogsDir,
		FramesDir:      p.Env.FramesDir(),
		MaxAgents:      p.MaxAgents,
		MaxChildren:    p.MaxChildren,
	})
	workflowService := agents.NewWorkflowService(agents.WorkflowParams{
		Workflows:    st.workflows,
		Agents:       st.agents,
		AgentDeleter: lifecycleService,
		Emptied:      emptyWorkflows,
		Schedules:    st.schedules,
		Tmux:         p.Tmux,
		Tx:           st.pool,
		Scripts:      scriptService,
		MaxAgents:    p.MaxAgents,
		MaxChildren:  p.MaxChildren,
		Notifier:     mailService,
	})

	return &services{
		loadConfig:   loadConfig,
		script:       scriptService,
		pullRequests: pullRequestOpener,
		mail:         mailService,
		review:       reviewService,
		schedule:     scheduleService,
		liveness:     paneLiveness,
		launch:       launchService,
		resume:       resumeService,
		adopt:        adoptService,
		lifecycle:    lifecycleService,
		workflow:     workflowService,
		delivery: agents.NewDeliveryService(agents.NewDeliveryServiceParams{
			MailboxReader: st.mail,
			NudgeClaimer:  st.agents,
			PaneReader:    paneLiveness,
			PaneTyper:     p.Tmux,
			Harnesses:     p.Harnesses,
		}),
		reconciler: agents.NewReconciler(p.Tmux, p.Harnesses, st.agents).
			WithPRFinder(agents.NewGHPRFinder()).
			WithFrameRecorder(frames.NewRecorder(p.Env.FramesDir(), newFrameSettings(loadConfig).get)),
	}, nil
}
