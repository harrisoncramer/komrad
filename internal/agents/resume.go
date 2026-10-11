package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/harness"
	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tasks"
)

// interruptedPrompt is what a resumed agent is asked on the way back in.
const interruptedPrompt = "Your last turn was cut off part-way through when your pane was stopped, " +
	"so nothing you had in flight was carried over. Read back over the conversation, work out what you " +
	"had actually finished and what you had not, and carry on from there. If the work was already done, " +
	"say so rather than doing it again."

// errResumeNotYours is an agent other than an orchestrator resuming an agent that is neither its task's
// writer, a reviewer on the task it writes for, nor its workflow's orchestrator.
var errResumeNotYours = fail.Denied("a reviewer may resume only its task's writer, a writer only its task's reviewers, and either its workflow's orchestrator")

// errResumeMoving is a resume into a checkout komrad is rebasing, pushing or aborting in.
var errResumeMoving = fail.Precondition("komrad is rebasing, pushing or aborting in this agent's checkout, so resume it once that finishes")

// treeLocker holds a checkout against a rebase, push or abort.
type treeLocker interface {
	TryLock(dir string) (func(), bool)
}

type NewResumeServiceParams struct {
	Tmux               paneController
	Agents             *agentstore.AgentStore
	Workflows          *workflowstore.WorkflowStore
	Launcher           *Launcher
	LaunchService      *LaunchService
	Harnesses          harnessGetter
	Scripts            agentScriptRunner
	Locks              treeLocker
	Transcripts        transcriptSyncer
	Tasks              *tasks.TaskService
	FallbackResumeRoot string
}

type ResumeService struct {
	tmux               paneController
	starter            *agentStarter
	launchService      *LaunchService
	agents             *agentstore.AgentStore
	workflows          *workflowstore.WorkflowStore
	harnesses          harnessGetter
	locks              treeLocker
	transcripts        transcriptSyncer
	tasks              *tasks.TaskService
	fallbackResumeRoot string
	// inFlight holds the agents this panel is part-way through resuming.
	inFlight   map[string]struct{}
	inFlightMu sync.Mutex
}

// NewResumeService builds the service from its dependencies.
func NewResumeService(params NewResumeServiceParams) *ResumeService {
	if params.FallbackResumeRoot == "" {
		params.FallbackResumeRoot = defaultFallbackResumeRoot()
	}

	return &ResumeService{
		tmux: params.Tmux,
		starter: &agentStarter{
			tmux:     params.Tmux,
			launcher: params.Launcher,
			scripts:  params.Scripts,
		},
		agents:             params.Agents,
		launchService:      params.LaunchService,
		workflows:          params.Workflows,
		harnesses:          params.Harnesses,
		locks:              params.Locks,
		transcripts:        params.Transcripts,
		tasks:              params.Tasks,
		fallbackResumeRoot: params.FallbackResumeRoot,
		inFlight:           map[string]struct{}{},
	}
}

// ResumeAgentParams is the agent to resume and the agent asking.
type ResumeAgentParams struct {
	ID string
	// ByAgentID is the agent asking, empty for the panel.
	ByAgentID string
}

// ResumeAgent relaunches a dormant agent in its previous conversation.
func (s *ResumeService) ResumeAgent(ctx context.Context, params ResumeAgentParams) (*agentstore.Agent, error) {
	agent, err := s.getByID(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	if err := s.refuseResumingOthers(ctx, params.ByAgentID, *agent); err != nil {
		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived, restore it first", agenterr.ErrAgentArchived, agent.Name)
	}

	// Relaunching a held agent would put it back to work under whoever is holding its worktree.
	if err := agent.ValidateActionable(); err != nil {
		return nil, err
	}

	if isPaneOwnedBy(s.tmux.GetPaneOwner(agent.Pane), agent.ID) {
		return agent, nil
	}
	release, err := s.lockTree(*agent)
	if err != nil {
		return nil, err
	}
	defer release()

	if err := s.resume(ctx, *agent); err != nil {
		return nil, err
	}

	return s.getByID(ctx, params.ID)
}

// refuseResumingOthers holds a reviewer to its task's writer, a writer to its task's reviewers, and
// either to its workflow's orchestrator.
func (s *ResumeService) refuseResumingOthers(ctx context.Context, byAgentID string, target agentstore.Agent) error {
	if byAgentID == "" {
		return nil
	}

	asker, err := s.agents.GetByID(ctx, byAgentID)
	if err != nil {
		return fmt.Errorf("failed to get the agent resuming: %w", err)
	}
	if asker.Role == agentstore.RoleOrchestrator {
		return nil
	}
	if isWriterAndReviewer(*asker, target) {
		return nil
	}
	if target.Role == agentstore.RoleOrchestrator && target.WorkflowID == asker.WorkflowID {
		return nil
	}

	return errResumeNotYours.Saying("%s may not resume %s", asker.Name, target.Name)
}

// lockTree holds a task writer's checkout for its launch, refusing one komrad is moving.
func (s *ResumeService) lockTree(agent agentstore.Agent) (func(), error) {
	if agent.Cwd == "" || !isTaskWriter(agent) {
		return func() {}, nil
	}

	release, taken := s.locks.TryLock(agent.Cwd)
	if !taken {
		return nil, errResumeMoving.Saying("%q works in %q, which komrad is moving", agent.Name, agent.Cwd)
	}

	return release, nil
}

// isTaskWriter reports whether the agent is the writer on a task.
func isTaskWriter(agent agentstore.Agent) bool {
	return agent.TaskID != nil && agent.IsWriter()
}

func (s *ResumeService) getByID(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, agenterr.ErrAgentNotFound.Saying("no agent with id %q", id)
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// takeInFlight takes the right to resume one agent, reporting false when this panel is already
// part-way through resuming it, and hands back the call that gives the right up.
func (s *ResumeService) takeInFlight(id string) (func(), bool) {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()

	if _, held := s.inFlight[id]; held {
		return nil, false
	}

	s.inFlight[id] = struct{}{}

	return func() {
		s.inFlightMu.Lock()
		defer s.inFlightMu.Unlock()
		delete(s.inFlight, id)
	}, true
}

func (s *ResumeService) resume(ctx context.Context, agent agentstore.Agent) error {
	// Answers written at once each ask for the same reviewer to be woken, and a second caller
	// getting this far is what would open a second window for one agent.
	done, taken := s.takeInFlight(agent.ID)
	if !taken {
		return fmt.Errorf("%w: %q is already being resumed", agenterr.ErrResumeInFlight, agent.Name)
	}
	defer done()

	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	// A tmux server killed whole takes every pane and the reconciler's view of them at once, so
	// the interrupted turn is stamped here rather than left to the reconciler.
	if agent.InterruptedAt == nil && agent.State.IsTurnUnfinished() {
		at := time.Now()
		if err := s.agents.SetInterruptedAtByID(ctx, agent.ID, &at); err != nil {
			return fmt.Errorf("failed to record the cut-off turn for %q: %w", agent.Name, err)
		}

		agent.InterruptedAt = &at
	}

	return s.relaunch(ctx, relaunchParams{agent: agent, harness: hn})
}

type relaunchParams struct {
	agent   agentstore.Agent
	harness harness.Harness
	restart *restartLaunch
}

// relaunch puts an agent back in a pane, on its own conversation or, when restarting, on a new one.
func (s *ResumeService) relaunch(ctx context.Context, params relaunchParams) error {
	agent := params.agent

	// Until this write the row reads as a dormant agent nobody is touching.
	live, err := s.agents.MarkSettingUpIfLive(ctx, agent.ID)
	if err != nil {
		return fmt.Errorf("failed to mark %q as resuming: %w", agent.Name, err)
	}
	if !live {
		return fmt.Errorf("%w: %q was archived while it was being resumed, restore it first", agenterr.ErrAgentArchived, agent.Name)
	}

	// Read before the resume starts overwriting the copy it was handed, which the restore below
	// puts back.
	prior := agent.State

	return s.starter.start(ctx, agent, &resumeStart{
		service: s,
		scripts: s.starter.scripts,
		harness: params.harness,
		prior:   prior,
		restart: params.restart,
	})
}

type resumeStart struct {
	service *ResumeService
	scripts agentScriptRunner
	harness harness.Harness
	prior   agentstore.State
	restart *restartLaunch
}

func (r *resumeStart) runSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	name := provisionerFor(ctx, r.service.workflows, agent)
	if r.scripts == nil || name == "" {
		return "", nil
	}

	return r.scripts.RunResume(ctx, agent, name)
}

func (r *resumeStart) getDefaultDir(agent agentstore.Agent) string {
	return r.service.getResumeDir(agent)
}

func (r *resumeStart) runPreHooks(ctx context.Context, agent agentstore.Agent) error {
	if err := (scriptrun.HookRun{
		Scripts: r.scripts,
		Agent:   agent,
		Hooks:   agentHooks(ctx, r.service.workflows, agent),
		Event:   workflowstore.HookAgentPreResume,
	}.RunInTurn(ctx)); err != nil {
		return fmt.Errorf("failed to run the pre-resume hook of %q: %w", agent.Name, err)
	}

	return nil
}

func (r *resumeStart) record(ctx context.Context, agent *agentstore.Agent, pane string) error {
	if r.restart != nil {
		if err := r.setHarness(ctx, *agent); err != nil {
			return fmt.Errorf("failed to start a new conversation for %q: %w", agent.Name, err)
		}
	}

	agent.Pane = pane
	agent.State = agentstore.StateSettingUp
	// An archive or a delete takes the row without reading the resume mark, so this write is where
	// a resume finds out it lost and has to take away the pane it opened.
	written, err := r.service.agents.UpdateIfLive(ctx, *agent)
	if err != nil {
		return fmt.Errorf("failed to resume agent %q: %w", agent.Name, err)
	}
	if !written {
		return fmt.Errorf("%w: %q was archived or deleted while it was being resumed", agenterr.ErrAgentArchived, agent.Name)
	}

	return nil
}

func (r *resumeStart) planLaunch(agent agentstore.Agent) paneLaunch {
	if r.restart != nil {
		return r.restart.plan(r.harness, agent)
	}

	// Codex takes no session id on the command line, so an agent may have none and its relaunch is
	// a fresh conversation with no turn to read back over.
	resuming := agent.SessionID != ""

	prompt := ""
	if resuming && agent.InterruptedAt != nil {
		prompt = interruptedPrompt
	}

	return paneLaunch{
		harness: r.harness,
		params: launchParams{
			Pane:      agent.Pane,
			Dir:       agent.Cwd,
			Flags:     agent.Flags,
			Prompt:    prompt,
			SessionID: agent.SessionID,
			AgentID:   agent.ID,
			Role:      agent.Role,
			Resume:    resuming,
		},
		verb: "relaunch",
	}
}

func (r *resumeStart) finish(ctx context.Context, agent agentstore.Agent) error {
	// A recorded turn does not outlive the relaunch, and a new conversation has none.
	if agent.InterruptedAt != nil || r.restart != nil {
		if err := r.service.agents.SetInterruptedAtByID(ctx, agent.ID, nil); err != nil {
			return fmt.Errorf("failed to clear the interrupted turn for %q: %w", agent.Name, err)
		}
	}

	scriptrun.HookRun{
		Scripts: r.scripts,
		Agent:   agent,
		Hooks:   agentHooks(ctx, r.service.workflows, agent),
		Event:   workflowstore.HookAgentPostResume,
	}.Start(ctx)

	return nil
}

// setHarness points the agent at its conversation, on the CLI and flags it names.
func (r *resumeStart) setHarness(ctx context.Context, agent agentstore.Agent) error {
	return r.service.agents.SetHarness(ctx, agentstore.SetHarnessParams{
		ID:        agent.ID,
		Harness:   agent.Harness,
		Flags:     agent.Flags,
		SessionID: agent.SessionID,
	})
}

// rollBack puts back the state the resume found, and the conversation, CLI and flags a restart
// left.
func (r *resumeStart) rollBack(ctx context.Context, agent agentstore.Agent, _ error) {
	if err := r.service.agents.SetStateByID(ctx, agent.ID, r.prior); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to put back the state of an agent whose resume gave up",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
	if r.restart == nil {
		return
	}
	if err := r.setHarness(ctx, r.restart.earlier); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to put back the conversation, CLI and flags of a writer whose restart gave up",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

func (s *ResumeService) getResumeDir(agent agentstore.Agent) string {
	dir := config.ExpandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if scripts.RequireUsableDir(dir) == nil {
		return dir
	}

	if err := os.MkdirAll(dir, 0o755); err == nil && scripts.RequireUsableDir(dir) == nil {
		return dir
	}

	standIn := filepath.Join(s.fallbackResumeRoot, agent.ID)
	if err := os.MkdirAll(standIn, 0o755); err != nil || scripts.RequireUsableDir(standIn) != nil {
		home, _ := os.UserHomeDir()

		return home
	}

	return standIn
}

// isWriterAndReviewer reports whether one of the two agents writes for the task the other reviews.
func isWriterAndReviewer(asker, target agentstore.Agent) bool {
	return (isTaskWriter(asker) && isReviewerOf(target, asker)) || (isTaskWriter(target) && isReviewerOf(asker, target))
}
