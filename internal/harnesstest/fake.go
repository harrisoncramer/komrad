package harnesstest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"weave/internal/harness"

	"github.com/google/uuid"
)

const FakeID = "claude"

const (
	FrameWorking      = "frame: working"
	FrameIdle         = "frame: idle"
	FrameComposing    = "frame: composing"
	FrameWaiting      = "frame: waiting"
	FrameUnrecognized = "frame: unrecognized"
)

// Fake is a harness that reads the frames above and records every launch it renders.
type Fake struct {
	mu         sync.Mutex
	Launches   []harness.Launch
	CommandErr error
	Name       string
	NoSession  bool
}

var _ harness.Harness = (*Fake)(nil)

// ID is Name, or FakeID when the test gave none.
func (f *Fake) ID() string {
	if f.Name != "" {
		return f.Name
	}

	return FakeID
}

// Binary is the harness's id.
func (f *Fake) Binary() string { return f.ID() }

// BuildCommand records the launch and renders a command naming its session.
func (f *Fake) BuildCommand(launch harness.Launch) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Launches = append(f.Launches, launch)
	if f.CommandErr != nil {
		return "", f.CommandErr
	}

	return "agent-cli --session " + launch.SessionID, nil
}

// MintSessionID is a fresh id, or nothing for a harness standing in for Codex.
func (f *Fake) MintSessionID() string {
	if f.NoSession {
		return ""
	}

	return uuid.NewString()
}

// InterruptKeys is Escape.
func (f *Fake) InterruptKeys() []string { return []string{"Escape"} }

// ReadReport reads nothing.
func (f *Fake) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}

// ReadInput reads one of the frames above, and any other frame as idle.
func (f *Fake) ReadInput(frame string) harness.Input {
	switch {
	case frame == FrameWorking:
		return harness.Input{Activity: harness.ActivityWorking}
	case frame == FrameWaiting:
		return harness.Input{Activity: harness.ActivityWaiting}
	case strings.HasPrefix(frame, FrameComposing):
		return harness.Input{Activity: harness.ActivityComposing, Held: composingLines(strings.TrimPrefix(frame, FrameComposing))}
	case frame == "":
		return harness.Input{Activity: harness.ActivityUnknown}
	case strings.HasPrefix(frame, FrameUnrecognized):
		return harness.Input{Activity: harness.ActivityUnrecognized}
	default:
		return harness.Input{Activity: harness.ActivityIdle, Held: []string{""}}
	}
}

func composingLines(held string) []string {
	if held == "" {
		return []string{"half a question about"}
	}

	return strings.Split(strings.TrimPrefix(held, ": "), "|")
}

// LogPath is empty.
func (f *Fake) LogPath(string) string { return "" }

// FindSession finds nothing.
func (f *Fake) FindSession(string) (string, error) { return "", nil }

// ReadLog reads nothing.
func (f *Fake) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

// Registry answers an id from ByID, then with Harness, refusing any id outside Only.
type Registry struct {
	Harness *Fake
	ByID    map[string]*Fake
	Only    []string
}

// GetByID is the harness registered under id.
func (r Registry) GetByID(id string) (harness.Harness, error) {
	if len(r.Only) > 0 && !slices.Contains(r.Only, id) {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
	}
	if found, ok := r.ByID[id]; ok {
		return found, nil
	}

	return r.Harness, nil
}

// Channel takes every line it is handed unless told to refuse, recording what reached it.
type Channel struct {
	RefuseReady bool
	RefuseSend  bool
	SendErr     error
	Sent        []string
	Forgotten   []string
	Interrupts  int
}

var _ harness.Channel = (*Channel)(nil)

// Ready is true unless RefuseReady is set.
func (c *Channel) Ready(context.Context, harness.Target) (bool, error) {
	return !c.RefuseReady, nil
}

// Send records text unless the channel was told to refuse it.
func (c *Channel) Send(_ context.Context, _ harness.Target, text string) (bool, error) {
	if c.SendErr != nil {
		return false, c.SendErr
	}
	if c.RefuseSend {
		return false, nil
	}
	c.Sent = append(c.Sent, text)

	return true, nil
}

// Interrupt counts the interrupt.
func (c *Channel) Interrupt(context.Context, harness.Target) error {
	c.Interrupts++

	return nil
}

// Forget records the agent forgotten.
func (c *Channel) Forget(agentID string) { c.Forgotten = append(c.Forgotten, agentID) }
