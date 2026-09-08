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

	"weave/config"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// interruptedPrompt is what a resumed agent is asked on the way back in, no CLI carrying an
// unfinished turn across a relaunch.
const interruptedPrompt = "Your last turn was cut off part-way through when your pane was stopped, " +
	"so nothing you had in flight was carried over. Read back over the conversation, work out what you " +
	"had actually finished and what you had not, and carry on from there. If the work was already done, " +
	"say so rather than doing it again."

// resumeAgentReadWriter is the slice of the agent store a resume writes to, which is the row's state
// and the pane it comes back in.
type resumeAgentReadWriter interface {
	agentGetter
	MarkSettingUpIfLive(ctx context.Context, id string) (bool, error)
	SetStateByID(ctx context.Context, id string, state agentstore.State) error
	SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error
	UpdateIfLive(ctx context.Context, agent agentstore.Agent) (bool, error)
}

type ResumeService struct {
	tmux               paneController
	agents             resumeAgentReadWriter
	workflows          workflowReader
	launcher           *Launcher
	harnesses          harnessGetter
	scripts            agentScriptRunner
	fallbackResumeRoot string
	// inFlight holds the agents this panel is part-way through resuming, which is what stops two
	// callers each opening a window for one agent, and is kept here rather than on the row.
	inFlight   map[string]struct{}
	inFlightMu sync.Mutex
}

type ResumeServiceParams struct {
	Tmux               paneController
	Agents             resumeAgentReadWriter
	Workflows          workflowReader
	Launcher           *Launcher
	Harnesses          harnessGetter
	Scripts            agentScriptRunner
	FallbackResumeRoot string
}

// NewResumeService builds the service from its dependencies.
func NewResumeService(p ResumeServiceParams) *ResumeService {
	if p.FallbackResumeRoot == "" {
		p.FallbackResumeRoot = defaultFallbackResumeRoot()
	}

	return &ResumeService{
		tmux:               p.Tmux,
		agents:             p.Agents,
		workflows:          p.Workflows,
		launcher:           p.Launcher,
		harnesses:          p.Harnesses,
		scripts:            p.Scripts,
		fallbackResumeRoot: p.FallbackResumeRoot,
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
		return nil, fmt.Errorf("%w: %q is archived, restore it first", ErrAgentArchived, agent.Name)
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
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// takeInFlight takes the right to resume one agent, reporting false when this panel is already
// part-way through resuming it, and hands back what gives that right up.
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
		return fmt.Errorf("%w: %q is already being resumed", ErrResumeInFlight, agent.Name)
	}
	defer done()

	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	// Until this write the row reads as a dormant agent nobody is touching, which is exactly what
	// a background pass archives.
	live, err := s.agents.MarkSettingUpIfLive(ctx, agent.ID)
	if err != nil {
		return fmt.Errorf("failed to mark %q as resuming: %w", agent.Name, err)
	}
	if !live {
		return fmt.Errorf("%w: %q was archived while it was being resumed, restore it first", ErrAgentArchived, agent.Name)
	}

	// Read before the resume starts overwriting the copy it was handed, which the restore below
	// puts back.
	prior := agent.State

	var pane string
	launched := false
	defer func() {
		if launched {
			return
		}

		// Nothing else in weave moves a row out of setting_up or takes away a window a resume
		// opened, so both are put back here rather than depending on the caller still being
		// around to see it happen.
		restore, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
		defer cancel()

		if pane != "" {
			s.tmux.KillPane(pane)
		}

		if err := s.agents.SetStateByID(restore, agent.ID, prior); err != nil {
			logging.From(restore).LogAttrs(restore, slog.LevelError, "failed to put back the state of an agent whose resume gave up",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}()

	named, err := s.runResumeSetup(ctx, agent)
	if err != nil {
		return err
	}

	dir, err := namedDir(named)
	if err != nil {
		return err
	}

	if dir == "" {
		dir = s.getResumeDir(agent)
	}

	pane, err = s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}
	if err := s.tmux.SetPaneOwner(pane, agent.ID); err != nil {
		return fmt.Errorf("failed to stamp the pane for %q: %w", agent.Name, err)
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

	agent.Pane = pane
	agent.State = agentstore.StateIdle
	agent.Cwd = dir
	// An archive or a delete takes the row without reading the resume mark, so this write is where
	// a resume finds out it lost and has to take away the pane it opened.
	written, err := s.agents.UpdateIfLive(ctx, agent)
	if err != nil {
		return fmt.Errorf("failed to resume agent %q: %w", agent.Name, err)
	}
	if !written {
		return fmt.Errorf("%w: %q was archived or deleted while it was being resumed", ErrAgentArchived, agent.Name)
	}

	// Codex takes no session id on the command line, so an agent may have none and its relaunch is
	// a fresh conversation with no turn to read back over.
	resuming := agent.SessionID != ""

	prompt := ""
	if resuming && agent.InterruptedAt != nil {
		prompt = interruptedPrompt
	}

	if err := s.launcher.launch(hn, launchParams{
		Pane:      pane,
		Dir:       dir,
		Flags:     agent.Flags,
		Prompt:    prompt,
		SessionID: agent.SessionID,
		AgentID:   agent.ID,
		Resume:    resuming,
	}); err != nil {
		return fmt.Errorf("failed to relaunch the agent for %q: %w", agent.Name, err)
	}

	launched = true

	// A recorded turn does not outlive the relaunch.
	if agent.InterruptedAt != nil {
		if err := s.agents.SetInterruptedAtByID(ctx, agent.ID, nil); err != nil {
			return fmt.Errorf("failed to clear the interrupted turn for %q: %w", agent.Name, err)
		}
	}

	startHooks(ctx, hookRunParams{
		scripts: s.scripts,
		agent:   agent,
		hooks:   agentHooks(ctx, s.workflows, agent),
		event:   workflowstore.HookPostResume,
	})

	return nil
}

func (s *ResumeService) runResumeSetup(ctx context.Context, agent agentstore.Agent) (string, error) {
	name := provisionerFor(ctx, s.workflows, agent)
	if s.scripts == nil || name == "" {
		return "", nil
	}

	return s.scripts.RunResume(ctx, agent, name)
}

func (s *ResumeService) getResumeDir(agent agentstore.Agent) string {
	dir := config.ExpandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if requireUsableDir(dir) == nil {
		return dir
	}

	if err := os.MkdirAll(dir, 0o755); err == nil && requireUsableDir(dir) == nil {
		return dir
	}

	standIn := filepath.Join(s.fallbackResumeRoot, agent.ID)
	if err := os.MkdirAll(standIn, 0o755); err != nil || requireUsableDir(standIn) != nil {
		home, _ := os.UserHomeDir()

		return home
	}

	return standIn
}
