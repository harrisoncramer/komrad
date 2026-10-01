package agents

import (
	"context"
	"fmt"

	"komrad/internal/harness"
	agentstore "komrad/internal/store/agent"
)

type agentStarter struct {
	tmux     paneController
	launcher *Launcher
	scripts  agentScriptRunner
}

type paneStart interface {
	runSetUp(ctx context.Context, agent agentstore.Agent) (string, error)
	getDefaultDir(agent agentstore.Agent) string
	runPreHooks(ctx context.Context, agent agentstore.Agent) error
	record(ctx context.Context, agent *agentstore.Agent, pane string) error
	planLaunch(agent agentstore.Agent) paneLaunch
	finish(ctx context.Context, agent agentstore.Agent) error
	rollBack(ctx context.Context, agent agentstore.Agent, cause error)
}

type paneLaunch struct {
	harness harness.Harness
	params  launchParams
	verb    string
}

func (s *agentStarter) start(ctx context.Context, agent agentstore.Agent, mode paneStart) (startErr error) {
	var pane string
	launched := false
	defer func() {
		if launched {
			return
		}

		teardown, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
		defer cancel()

		if pane != "" {
			s.tmux.KillPane(pane)
		}
		mode.rollBack(teardown, agent, startErr)
	}()

	named, err := mode.runSetUp(ctx, agent)
	if err != nil {
		return err
	}

	dir, err := namedDir(named)
	if err != nil {
		return err
	}
	if dir == "" {
		dir = mode.getDefaultDir(agent)
	}
	agent.Cwd = dir

	if err := mode.runPreHooks(ctx, agent); err != nil {
		return err
	}

	pane, err = s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}

	if err := s.tmux.SetPaneOwner(pane, agent.ID); err != nil {
		return fmt.Errorf("failed to stamp the pane for %q: %w", agent.Name, err)
	}

	if err := mode.record(ctx, &agent, pane); err != nil {
		return err
	}

	launch := mode.planLaunch(agent)
	if err := s.launcher.launch(ctx, launch.harness, launch.params); err != nil {
		return fmt.Errorf("failed to %s the agent for %q: %w", launch.verb, agent.Name, err)
	}

	launched = true

	return mode.finish(ctx, agent)
}
