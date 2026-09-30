package agents

import (
	"context"
	"log/slog"
	"sync"

	"komrad/internal/frames"
	"komrad/internal/harness"
	"komrad/internal/logging"

	agentstore "komrad/internal/store/agent"
	screenstore "komrad/internal/store/screen"
)

// screenCapturer reads a pane's whole visible grid and its size.
type screenCapturer interface {
	CaptureScreen(pane string) string
	GetPaneWidth(pane string) int
	GetPaneHeight(pane string) int
}

type screenStore interface {
	Upsert(ctx context.Context, params screenstore.UpsertParams) error
	Get(ctx context.Context, agentID string) (*screenstore.Screen, error)
}

// screenKeeper saves the last screen each agent's pane drew at rest.
type screenKeeper interface {
	Keep(ctx context.Context, params KeepParams)
	Capture(ctx context.Context, agent agentstore.Agent)
	Forget(listed map[string]struct{})
}

type NewScreenKeeperParams struct {
	Panes   screenCapturer
	Screens screenStore
}

// ScreenKeeper saves an agent's screen whenever the frame it rests on changes.
type ScreenKeeper struct {
	panes   screenCapturer
	screens screenStore

	// mu guards the hash of the frame each agent's saved screen was taken on.
	mu    sync.Mutex
	saved map[string]uint64
}

// NewScreenKeeper builds a keeper capturing through panes and saving to screens.
func NewScreenKeeper(params NewScreenKeeperParams) *ScreenKeeper {
	return &ScreenKeeper{
		panes:   params.Panes,
		screens: params.Screens,
		saved:   map[string]uint64{},
	}
}

type KeepParams struct {
	Agent    agentstore.Agent
	Frame    string
	Activity harness.Activity
}

// Keep saves the agent's screen when it reads as resting on a frame not saved yet.
func (k *ScreenKeeper) Keep(ctx context.Context, params KeepParams) {
	if !isResting(params.Activity) || params.Frame == "" {
		return
	}

	hash := frames.Hash(params.Frame)

	k.mu.Lock()
	seen, ok := k.saved[params.Agent.ID]
	k.mu.Unlock()

	if ok && seen == hash {
		return
	}

	if k.save(ctx, params.Agent) {
		k.mu.Lock()
		k.saved[params.Agent.ID] = hash
		k.mu.Unlock()
	}
}

// Capture saves the agent's screen as its pane shows it now.
func (k *ScreenKeeper) Capture(ctx context.Context, agent agentstore.Agent) {
	k.save(ctx, agent)
}

// Forget drops what the keeper holds for agents no longer listed.
func (k *ScreenKeeper) Forget(listed map[string]struct{}) {
	k.mu.Lock()
	defer k.mu.Unlock()

	for id := range k.saved {
		if _, ok := listed[id]; !ok {
			delete(k.saved, id)
		}
	}
}

// GetLastScreen returns the screen last saved for an agent.
func (k *ScreenKeeper) GetLastScreen(ctx context.Context, agentID string) (*screenstore.Screen, error) {
	return k.screens.Get(ctx, agentID)
}

func (k *ScreenKeeper) save(ctx context.Context, agent agentstore.Agent) bool {
	if agent.Pane == "" {
		return false
	}

	frame := k.panes.CaptureScreen(agent.Pane)
	width, height := k.panes.GetPaneWidth(agent.Pane), k.panes.GetPaneHeight(agent.Pane)
	if frame == "" || width <= 0 || height <= 0 {
		return false
	}

	if err := k.screens.Upsert(ctx, screenstore.UpsertParams{
		AgentID: agent.ID,
		Frame:   frame,
		Width:   width,
		Height:  height,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to save an agent's screen",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return false
	}

	return true
}

func isResting(activity harness.Activity) bool {
	switch activity {
	case harness.ActivityIdle, harness.ActivityComposing, harness.ActivityWaiting:
		return true
	case harness.ActivityUnknown, harness.ActivityWorking, harness.ActivityUnrecognized:
	}

	return false
}

type captureOwnedScreensParams struct {
	tmux    paneLister
	screens screenKeeper
	agents  []agentstore.Agent
}

// captureOwnedScreens saves the screen of each agent whose pane still runs its CLI.
func captureOwnedScreens(ctx context.Context, params captureOwnedScreensParams) {
	if params.screens == nil {
		return
	}

	panes, err := params.tmux.GetPanes()
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read panes, saving no screens",
			logging.Err(err)...)

		return
	}

	for _, agent := range params.agents {
		pane := panes[agent.Pane]
		if isPaneOwnedBy(pane.Owner, agent.ID) && !pane.IsRunningShell() {
			params.screens.Capture(ctx, agent)
		}
	}
}
