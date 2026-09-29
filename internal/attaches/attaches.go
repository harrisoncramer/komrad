// Package attaches shows an agent's terminal on the engine to a person at a terminal somewhere
// else, through a view session of the agent's own that shares the engine's windows.
package attaches

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"komrad/internal/engine"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/pty"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/clock"
)

const (
	// detachGrace is how long a view is watched for its last client to leave before it is kept.
	detachGrace = time.Second
	// detachPoll is how often the view is looked at while it is watched.
	detachPoll = 50 * time.Millisecond
)

var (
	// ErrNotLive refuses an attach to an agent that is archived or being deleted.
	ErrNotLive = fail.Precondition("the agent is not live")
	// ErrNoPane refuses an attach to an agent that holds no pane of its own.
	ErrNoPane = fail.Precondition("the agent holds no pane")
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type viewAttacher interface {
	GetPaneOwner(pane string) string
	OpenView(view, session, pane string) error
	CountSessionClients(session string) (int, error)
	KillSession(session string) error
	BuildAttachArgv(session string) []string
}

// NewAttachServiceParams is what the service is built over.
type NewAttachServiceParams struct {
	Agents  agentGetter
	Tmux    viewAttacher
	Session string
}

// AttachService attaches terminals to the panes agents run in.
type AttachService struct {
	agents  agentGetter
	tmux    viewAttacher
	session string

	mu      sync.Mutex
	streams map[string]int
}

// NewAttachService builds the service.
func NewAttachService(params NewAttachServiceParams) *AttachService {
	return &AttachService{
		agents:  params.Agents,
		tmux:    params.Tmux,
		session: params.Session,
		streams: map[string]int{},
	}
}

// OpenParams is the attach a person asked for.
type OpenParams struct {
	AgentID string
	Size    pty.Size
	Term    string
}

// Open attaches a terminal to the pane of a live agent, detaching whatever terminal was attached
// to it before, killed when ctx ends.
func (s *AttachService) Open(ctx context.Context, params OpenParams) (pty.Terminal, error) {
	agent, err := s.agents.GetByID(ctx, params.AgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to find agent %s: %w", params.AgentID, err)
	}

	if agent.ArchivedAt != nil || agent.DeletingAt != nil {
		return nil, ErrNotLive.Saying("agent %s is not live", agent.Name)
	}

	if agent.Pane == "" || s.tmux.GetPaneOwner(agent.Pane) != agent.ID {
		return nil, ErrNoPane.Saying("agent %s holds no pane", agent.Name)
	}

	view := engine.ViewSession(agent.Pane)
	if err := s.claim(view, agent.Pane); err != nil {
		return nil, err
	}

	session, err := pty.Start(ctx, pty.StartParams{
		Argv: s.tmux.BuildAttachArgv(view),
		Env:  stripTmuxEnv(os.Environ()),
		Size: params.Size,
		Term: params.Term,
	})
	if err != nil {
		s.release(ctx, view)

		return nil, fmt.Errorf("failed to attach to agent %s: %w", agent.Name, err)
	}

	return &attached{Session: session, release: func() { s.release(ctx, view) }}, nil
}

func (s *AttachService) claim(view, pane string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.tmux.OpenView(view, s.session, pane); err != nil {
		return fmt.Errorf("failed to open a view of pane %s: %w", pane, err)
	}

	s.streams[view]++

	return nil
}

// release kills a view once its last stream has ended and no client is left attached to it.
func (s *AttachService) release(ctx context.Context, view string) {
	if !s.drop(view) || !s.isDetached(context.WithoutCancel(ctx), view) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.streams[view] > 0 {
		return
	}

	if err := s.tmux.KillSession(view); err != nil {
		ctx = context.WithoutCancel(ctx)
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "a view outlived its last attach", append(logging.Err(err), slog.String("view", view))...)
	}
}

// drop counts a stream off a view and reports whether it was the last.
func (s *AttachService) drop(view string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.streams[view]--
	if s.streams[view] > 0 {
		return false
	}

	delete(s.streams, view)

	return true
}

// isDetached reports whether a view is left with no client attached, waiting a moment for tmux to
// see a client that was killed go.
func (s *AttachService) isDetached(ctx context.Context, view string) bool {
	deadline := clock.Now().Add(detachGrace)
	for {
		clients, err := s.tmux.CountSessionClients(view)
		if err != nil {
			return false
		}

		if clients == 0 {
			return true
		}

		if clock.Now().After(deadline) {
			return false
		}

		clock.Sleep(ctx, detachPoll)
	}
}

// attached is a terminal attached to a view, which lets the view go once it has ended.
type attached struct {
	*pty.Session

	release func()
	once    sync.Once
}

// Wait waits for the attach to end and lets its view go.
func (a *attached) Wait() (int, error) {
	code, err := a.Session.Wait()
	a.once.Do(a.release)

	return code, err
}

// stripTmuxEnv removes the variables that tell tmux it is already inside one.
func stripTmuxEnv(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "TMUX=") || strings.HasPrefix(entry, "TMUX_PANE=") {
			continue
		}

		kept = append(kept, entry)
	}

	return kept
}
