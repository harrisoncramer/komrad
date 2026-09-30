package harnesstest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"komrad/internal/harness"
	"komrad/lib/xslice"

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

var (
	_ harness.Harness  = (*Fake)(nil)
	_ harness.Channel  = (*Channel)(nil)
	_ harness.Observer = (*Observer)(nil)
)

// Fake is a harness that reads the frames above and records every launch it renders.
type Fake struct {
	mu         sync.Mutex
	Launches   []harness.Launch
	Trusted    []string
	TrustErr   error
	CommandErr error
	Name       string
	NoSession  bool
	// InterruptWith is the keys an interrupt sends, Escape when nil.
	InterruptWith []string
	// Session is the id FindSession answers.
	Session string
	// Anonymous leaves ID blank.
	Anonymous bool
}

// ID is Name, or FakeID when the test gave none.
func (f *Fake) ID() string {
	if f.Anonymous {
		return ""
	}
	if f.Name != "" {
		return f.Name
	}

	return FakeID
}

// Binary is the harness's id.
func (f *Fake) Binary() string {
	return f.ID()
}

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

// TrustDir records the directory, failing with TrustErr.
func (f *Fake) TrustDir(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Trusted = append(f.Trusted, dir)

	return f.TrustErr
}

// MintSessionID is a fresh id, or nothing for a harness standing in for Codex.
func (f *Fake) MintSessionID() string {
	if f.NoSession {
		return ""
	}

	return uuid.NewString()
}

// InterruptKeys is InterruptWith, or Escape when the test gave none.
func (f *Fake) InterruptKeys() []string {
	if f.InterruptWith == nil {
		return []string{"Escape"}
	}

	return f.InterruptWith
}

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
		return harness.Input{Activity: harness.ActivityComposing, Held: splitComposingLines(strings.TrimPrefix(frame, FrameComposing))}
	case frame == "":
		return harness.Input{Activity: harness.ActivityUnknown}
	case strings.HasPrefix(frame, FrameUnrecognized):
		return harness.Input{Activity: harness.ActivityUnrecognized}
	default:
		return harness.Input{Activity: harness.ActivityIdle, Held: []string{""}}
	}
}

// LogPath is empty.
func (f *Fake) LogPath(string) string {
	return ""
}

// FindSession answers Session.
func (f *Fake) FindSession(string) (string, error) {
	return f.Session, nil
}

// ReadLog reads nothing.
func (f *Fake) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

// Registry answers an id from ByID, then with Harness, refusing any id outside Only or left with no harness.
type Registry struct {
	Harness *Fake
	ByID    map[string]*Fake
	Only    []string
	// BeforeGet runs ahead of every lookup, and an error it returns refuses that lookup.
	BeforeGet func(id string) error
}

// GetByID is the harness registered under id.
func (r Registry) GetByID(id string) (harness.Harness, error) {
	if r.BeforeGet != nil {
		if err := r.BeforeGet(id); err != nil {
			return nil, err
		}
	}
	if len(r.Only) > 0 && !slices.Contains(r.Only, id) {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
	}
	if found, ok := r.ByID[id]; ok {
		return found, nil
	}
	if r.Harness == nil {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
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
func (c *Channel) Forget(agentID string) {
	c.Forgotten = append(c.Forgotten, agentID)
}

func splitComposingLines(held string) []string {
	if held == "" {
		return []string{"half a question about"}
	}

	return strings.Split(strings.TrimPrefix(held, ": "), "|")
}

// Observer answers a run of observations, holding the last once they are spent, or Err in their place.
type Observer struct {
	mu           sync.Mutex
	Observations []harness.Observation
	Err          error
	Reads        int
	// Exited says the agent's CLI has left its pane.
	Exited bool
	// BeforeFirst runs ahead of the first observation.
	BeforeFirst func()
	began       bool
}

// Observe answers the next observation in the run.
func (o *Observer) Observe(context.Context, harness.Target) (harness.Observation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.began && o.BeforeFirst != nil {
		o.BeforeFirst()
	}
	o.began = true
	if o.Err != nil {
		return harness.Observation{}, o.Err
	}
	if len(o.Observations) == 0 {
		return harness.Observation{}, nil
	}

	at := min(o.Reads, len(o.Observations)-1)
	o.Reads++

	return o.Observations[at], nil
}

// IsRunning answers whether the CLI is still in its pane.
func (o *Observer) IsRunning(context.Context, harness.Target) bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	return !o.Exited
}

// NewActivityObserver is an observer answering a run of bare activities.
func NewActivityObserver(activities ...harness.Activity) *Observer {
	return &Observer{Observations: xslice.Map(activities, func(activity harness.Activity) harness.Observation {
		return harness.Observation{Activity: activity}
	})}
}
