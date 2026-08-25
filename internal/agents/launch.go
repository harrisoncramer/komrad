package agents

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"weave/config"
	"weave/internal/harness"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xstr"
)

type launchPlan struct {
	name            string
	harness         harness.Harness
	harnessID       string
	dir             string
	flags           string
	workflowContext string
	setupAction     string
	cleanupAction   string
}

func (s *AgentService) planLaunch(ctx context.Context, p NewAgentParams) (*launchPlan, error) {
	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}
	if wf == nil {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}

	harnessID := strings.TrimSpace(p.Harness)
	if harnessID == "" {
		harnessID = strings.TrimSpace(wf.Harness)
	}
	if harnessID == "" {
		harnessID = s.userDefaultHarness
	}

	hn, err := s.harnesses.GetByID(harnessID)
	if err != nil {
		return nil, err
	}

	dir := config.ExpandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = config.ExpandHome(strings.TrimSpace(wf.Cwd))
	}
	if dir == "" {
		dir = s.tmux.GetClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	if err := requireUsableDir(dir); err != nil {
		return nil, err
	}

	agentFlags := strings.TrimSpace(p.Flags)
	if agentFlags == "" {
		agentFlags = strings.TrimSpace(wf.Flags)
	}

	setupAction := wf.SetupAction
	if p.SetupAction != nil {
		setupAction = strings.TrimSpace(*p.SetupAction)
	}

	cleanupAction := wf.CleanupAction
	if p.CleanupAction != nil {
		cleanupAction = strings.TrimSpace(*p.CleanupAction)
	}

	return &launchPlan{
		name:            s.uniqueAgentName(ctx, p.WorkflowID, p.Title, p.Prompt),
		harness:         hn,
		harnessID:       harnessID,
		dir:             dir,
		flags:           strings.TrimSpace(s.userDefaultFlags[harnessID] + " " + agentFlags),
		workflowContext: wf.Context,
		setupAction:     setupAction,
		cleanupAction:   cleanupAction,
	}, nil
}

func (plan *launchPlan) agent(params NewAgentParams, id, sessionID, dir, pane string, spawner *agentstore.Agent) agentstore.Agent {
	var spawnedBy *string
	if spawner != nil {
		spawnedBy = &spawner.ID
	}

	return agentstore.Agent{
		ID:               id,
		Name:             plan.name,
		Pane:             pane,
		Cwd:              dir,
		Flags:            plan.flags,
		SetupAction:      params.SetupAction,
		CleanupAction:    params.CleanupAction,
		Prompt:           params.Prompt,
		State:            agentstore.StateIdle,
		Harness:          plan.harnessID,
		SessionID:        sessionID,
		WorkflowID:       params.WorkflowID,
		ScheduleID:       params.ScheduleID,
		IdempotencyKey:   idempotencyKey(params.IdempotencyKey),
		SpawnedByAgentID: spawnedBy,
		Hidden:           params.Hidden,
	}
}

func (s *AgentService) launchAgent(ctx context.Context, p NewAgentParams, spawner *agentstore.Agent) (result *agentstore.Agent, launchErr error) {
	plan, err := s.planLaunch(ctx, p)
	if err != nil {
		return nil, err
	}

	id, err := store.NewID()
	if err != nil {
		return nil, err
	}
	sessionID := plan.harness.MintSessionID()
	agent := plan.agent(p, id, sessionID, plan.dir, "", spawner)
	agent.State = agentstore.StateSettingUp

	if err := s.createUnderCeiling(ctx, agent); err != nil {
		return nil, err
	}

	var pane string
	pastSetup := false
	launched := false
	defer func() {
		if launched {
			return
		}

		// Nothing else in weave moves a row out of setting_up, so closing out a failed
		// create cannot depend on the caller still being around to see it happen.
		teardown, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
		defer cancel()

		if pane != "" {
			s.tmux.KillPane(pane)
		}
		if pastSetup {
			s.runCleanup(teardown, agent, plan.cleanupAction, nil)
		}
		s.failCreate(teardown, agent, launchErr)
	}()

	setupName, err := s.runSetup(ctx, agent, plan.setupAction, p.SetupPane)
	if err != nil {
		return nil, err
	}
	pastSetup = true

	dir, err := s.setupDir(plan.dir, setupName)
	if err != nil {
		return nil, err
	}
	agent = plan.agent(p, id, sessionID, dir, "", spawner)

	pane, err = s.tmux.NewWindow(ManagerSession, plan.name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", plan.name, err)
	}
	agent = plan.agent(p, id, sessionID, dir, pane, spawner)

	if err := s.tmux.SetPaneOwner(pane, id); err != nil {
		return nil, fmt.Errorf("failed to stamp the pane for %q: %w", plan.name, err)
	}

	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, fmt.Errorf("failed to record agent %q: %w", plan.name, err)
	}

	created, err := s.agents.GetInWorkflow(ctx, p.WorkflowID, plan.name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back agent %q: %w", plan.name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("agent %q vanished after being recorded", plan.name)
	}

	params := launchParams{
		Pane:            pane,
		Dir:             agent.Cwd,
		Flags:           plan.flags,
		Prompt:          p.Prompt,
		WorkflowContext: plan.workflowContext,
		SessionID:       sessionID,
		AgentID:         id,
	}
	if spawner != nil {
		params.SpawnedByAgentID = spawner.ID
		params.SpawnedByName = spawner.Name
	}

	if err := s.launcher.launch(plan.harness, params); err != nil {
		return nil, fmt.Errorf("failed to launch the agent for %q: %w", plan.name, err)
	}

	launched = true

	return created, nil
}

const maxFailureReasonLen = 480

// teardownTimeout stands in for the caller's own deadline once that no longer applies, so a
// wedged database cannot hold a failed create open indefinitely.
const teardownTimeout = 30 * time.Second

func (s *AgentService) failCreate(ctx context.Context, agent agentstore.Agent, cause error) {
	s.failAs(ctx, agent, agentstore.StateFailedToCreate, "create", cause)
}

func (s *AgentService) failCleanup(ctx context.Context, agent agentstore.Agent, cause error) {
	s.failAs(ctx, agent, agentstore.StateFailedToCleanup, "cleanup", cause)
}

// failAs marks an agent as having failed create or cleanup and leaves the row where it was. A
// delete that cannot finish leaves an archived row archived: the archive is what the user was
// clearing, so it is the archive that has to show what is left in it. "Archive and delete"
// archives first, so a failure there stays there too rather than resurrecting the agent.
func (s *AgentService) failAs(ctx context.Context, agent agentstore.Agent, state, verb string, cause error) {
	agent.State = state
	if err := s.agents.Update(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, fmt.Sprintf("failed to record why an agent's %s failed", verb),
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return
	}

	reason := failureReason(cause)
	if _, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: agent.ID, Key: "error", Value: &reason}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, fmt.Sprintf("failed to record why an agent's %s failed", verb),
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

func failureReason(cause error) string {
	if cause == nil {
		return ""
	}

	text := strings.Join(strings.Fields(cause.Error()), " ")
	if len(text) > maxFailureReasonLen {
		text = text[:maxFailureReasonLen]
	}

	return text
}

func (s *AgentService) runSetup(ctx context.Context, planned agentstore.Agent, name, pane string) (string, error) {
	if s.lifecycle == nil || name == "" {
		return "", nil
	}

	return s.lifecycle.RunSetupAction(ctx, planned, name, pane)
}

func (s *AgentService) setupDir(planned, named string) (string, error) {
	if named == "" {
		return planned, nil
	}

	return namedDir(named)
}

func namedDir(named string) (string, error) {
	if named == "" {
		return "", nil
	}

	dir := config.ExpandHome(named)
	if err := requireUsableDir(dir); err != nil {
		return "", fmt.Errorf("%w: a setup action named %q: %w", ErrSetupFailed, dir, err)
	}

	return dir, nil
}

func (s *AgentService) createUnderCeiling(ctx context.Context, agent agentstore.Agent) error {
	return s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore := agentstore.New(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, 1); err != nil {
			return err
		}

		if err := agentStore.Create(ctx, agent); err != nil {
			return fmt.Errorf("failed to record agent %q: %w", agent.Name, err)
		}

		return nil
	})
}

func requireUsableDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrBadDirectory, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrBadDirectory, dir)
	}

	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrBadDirectory, dir, err)
	}
	_ = handle.Close()

	return nil
}

func claimRoomForAgents(ctx context.Context, agents agentSlots, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForAgents(ctx, agents, maxAgents, want)
}

func requireRoomForAgents(ctx context.Context, agents agentSlots, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}

	live, err := agents.CountLive(ctx)
	if err != nil {
		return fmt.Errorf("failed to count the agents already running: %w", err)
	}
	if live+int64(want) > int64(maxAgents) {
		return fmt.Errorf("%w: max number of concurrent agents reached (%d of %d), please archive one to continue", ErrTooManyAgents, live, maxAgents)
	}

	return nil
}

func (s *AgentService) uniqueAgentName(ctx context.Context, workflowID, title, prompt string) string {
	base := agentstore.SanitizeName(strings.TrimSpace(title))
	if base == "" {
		base = xstr.Slug(xstr.FirstLine(prompt), 24)
	}
	if base == "" {
		base = "agent"
	}

	name := base
	for i := 2; s.isNameTaken(ctx, workflowID, name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

func (s *AgentService) isNameTaken(ctx context.Context, workflowID, name string) bool {
	taken, err := s.agents.ActiveNameExists(ctx, workflowID, name)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to check whether an agent name is taken",
			append([]slog.Attr{slog.String(logging.WorkflowID, workflowID)}, logging.Err(err)...)...)
	}

	return taken
}

func idempotencyKey(key string) *string {
	if key == "" {
		return nil
	}

	return &key
}
