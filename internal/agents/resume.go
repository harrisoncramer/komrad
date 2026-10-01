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
	"komrad/internal/harness"
	"komrad/internal/logging"
	"komrad/internal/scripts"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

// interruptedPrompt is what a resumed agent is asked on the way back in.
const interruptedPrompt = "Your last turn was cut off part-way through when your pane was stopped, " +
	"so nothing you had in flight was carried over. Read back over the conversation, work out what you " +
	"had actually finished and what you had not, and carry on from there. If the work was already done, " +
	"say so rather than doing it again."

// resumeAgentReadWriter is the slice of the agent store a resume writes to, being the row's
// state and the pane it comes back in.
type resumeAgentReadWriter interface {
	agentGetter
	MarkSettingUpIfLive(ctx context.Context, id string) (bool, error)
	SetStateByID(ctx context.Context, id string, state agentstore.State) error
	SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error
	UpdateIfLive(ctx context.Context, agent agentstore.Agent) (bool, error)
}

type NewResumeServiceParams struct {
	Tmux               paneController
	Agents             resumeAgentReadWriter
	Workflows          workflowReader
	Launcher           *Launcher
	Harnesses          harnessGetter
	Scripts            agentScriptRunner
	FallbackResumeRoot string
}

type ResumeService struct {
	tmux               paneController
	starter            *agentStarter
	agents             resumeAgentReadWriter
	workflows          workflowReader
	harnesses          harnessGetter
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
		workflows:          params.Workflows,
		harnesses:          params.Harnesses,
		fallbackResumeRoot: params.FallbackResumeRoot,
		inFlight:           map[string]struct{}{},
	}
}

// ResumeAgent relaunches a dormant agent in its previous conversation.
func (s *ResumeService) ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.getByID(ctx, id)
	if err != nil {
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

	if err := s.resume(ctx, *agent); err != nil {
		return nil, err
	}

	return s.getByID(ctx, id)
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
		harness: hn,
		prior:   prior,
	})
}

type resumeStart struct {
	service *ResumeService
	scripts agentScriptRunner
	harness harness.Harness
	prior   agentstore.State
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
	if err := runPreHooks(ctx, hookRunParams{
		scripts: r.scripts,
		agent:   agent,
		hooks:   agentHooks(ctx, r.service.workflows, agent),
		event:   workflowstore.HookPreResume,
	}); err != nil {
		return fmt.Errorf("failed to run the pre-resume hook of %q: %w", agent.Name, err)
	}

	return nil
}

func (r *resumeStart) record(ctx context.Context, agent *agentstore.Agent, pane string) error {
	// A tmux server killed whole takes every pane and the reconciler's view of them at once, so
	// the interrupted turn is stamped here rather than left to the reconciler.
	if agent.InterruptedAt == nil && agent.State.IsTurnUnfinished() {
		at := time.Now()
		if err := r.service.agents.SetInterruptedAtByID(ctx, agent.ID, &at); err != nil {
			return fmt.Errorf("failed to record the cut-off turn for %q: %w", agent.Name, err)
		}

		agent.InterruptedAt = &at
	}

	agent.Pane = pane
	agent.State = agentstore.StateIdle
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
	// A recorded turn does not outlive the relaunch.
	if agent.InterruptedAt != nil {
		if err := r.service.agents.SetInterruptedAtByID(ctx, agent.ID, nil); err != nil {
			return fmt.Errorf("failed to clear the interrupted turn for %q: %w", agent.Name, err)
		}
	}

	startHooks(ctx, hookRunParams{
		scripts: r.scripts,
		agent:   agent,
		hooks:   agentHooks(ctx, r.service.workflows, agent),
		event:   workflowstore.HookPostResume,
	})

	return nil
}

// rollBack puts back the state the resume found.
func (r *resumeStart) rollBack(ctx context.Context, agent agentstore.Agent, _ error) {
	if err := r.service.agents.SetStateByID(ctx, agent.ID, r.prior); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to put back the state of an agent whose resume gave up",
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
