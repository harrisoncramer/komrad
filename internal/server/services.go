package server

import (
	"fmt"
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/agents"
)

type services struct {
	loadConfig func() (*config.Config, error)
	action     *agents.ActionService
	mail       *agents.MailService
	review     *agents.ReviewService
	schedule   *agents.ScheduleService
	liveness   *agents.PaneLiveness
	launch     *agents.LaunchService
	resume     *agents.ResumeService
	adopt      *agents.AdoptService
	lifecycle  *agents.LifecycleService
	workflow   *agents.WorkflowService
	delivery   *agents.DeliveryService
	reconciler *agents.Reconciler
}

func buildServices(p Params, st *stores) (*services, error) {
	loadConfig := func() (*config.Config, error) { return config.Load(p.ConfigPaths) }

	mailService := agents.NewMailService(agents.NewMailServiceParams{
		MailReadWriter: st.mail,
		AgentFinder:    st.agents,
		Config:         mailConfig(loadConfig),
	})
	reviewService := agents.NewReviewService(agents.ReviewServiceParams{
		Threads:  st.review,
		Messages: st.review,
		Viewed:   st.review,
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

	builtinActionsDir := filepath.Join(p.Env.CacheDir, "weave-builtin-actions")
	if err := actions.InstallBuiltins(builtinActionsDir); err != nil {
		return nil, err
	}

	actionLogsDir := p.Env.ActionLogsDir()
	if err := os.MkdirAll(actionLogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", actionLogsDir, err)
	}

	actionService := agents.NewActionService(agents.ActionParams{
		Agents:     st.agents,
		Workflows:  st.workflows,
		Tmux:       p.Tmux,
		Runner:     actions.NewRunner(),
		Load:       loadConfig,
		Addr:       p.Addr,
		Socket:     p.Env.TmuxSocket,
		BuiltinDir: builtinActionsDir,
		LogsDir:    actionLogsDir,
		Editor:     p.Env.Editor,
		BinPath:    p.HookCommand,
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
		Actions:            actionService,
		UserDefaultHarness: p.DefaultHarness,
		UserDefaultFlags:   p.DefaultFlags,
		MaxAgents:          p.MaxAgents,
	})
	resumeService := agents.NewResumeService(agents.ResumeServiceParams{
		Tmux:               p.Tmux,
		Agents:             st.agents,
		Workflows:          st.workflows,
		Launcher:           harnessLauncher,
		Harnesses:          p.Harnesses,
		Actions:            actionService,
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
		Actions:        actionService,
		Notifier:       mailService,
		LogsDir:        actionLogsDir,
		MaxAgents:      p.MaxAgents,
	})
	workflowService := agents.NewWorkflowService(agents.WorkflowParams{
		Workflows:    st.workflows,
		Agents:       st.agents,
		AgentDeleter: lifecycleService,
		Emptied:      emptyWorkflows,
		Schedules:    st.schedules,
		Tmux:         p.Tmux,
		Tx:           st.pool,
		MaxAgents:    p.MaxAgents,
		Notifier:     mailService,
	})

	return &services{
		loadConfig: loadConfig,
		action:     actionService,
		mail:       mailService,
		review:     reviewService,
		schedule:   scheduleService,
		liveness:   paneLiveness,
		launch:     launchService,
		resume:     resumeService,
		adopt:      adoptService,
		lifecycle:  lifecycleService,
		workflow:   workflowService,
		delivery: agents.NewDeliveryService(agents.NewDeliveryServiceParams{
			MailboxReader: st.mail,
			NudgeClaimer:  st.agents,
			PaneReader:    paneLiveness,
			PaneTyper:     p.Tmux,
			Harnesses:     p.Harnesses,
		}),
		reconciler: agents.NewReconciler(p.Tmux, p.Harnesses, st.agents).WithPRFinder(agents.NewGHPRFinder()),
	}, nil
}
