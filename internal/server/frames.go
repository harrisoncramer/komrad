package server

import (
	"sync"
	"time"

	"weave/config"
	"weave/internal/frames"
)

// frameSettingsTTL is how long the recording settings are held before the config is read
// again, since the reconcile pass asks for them once per agent and turning a recording on is
// not something anybody needs to take effect the same second.
const frameSettingsTTL = 5 * time.Second

// frameSettings reads whether frames are being recorded from the config on disk, so a
// recording can be turned on and off under a service that is already running.
type frameSettings struct {
	load func() (*config.Config, error)
	now  func() time.Time

	// mu guards the settings held between reads, which the reconcile pass reaches for every
	// agent in turn.
	mu   sync.Mutex
	read time.Time
	held frames.Settings
}

func newFrameSettings(load func() (*config.Config, error)) *frameSettings {
	return &frameSettings{load: load, now: time.Now}
}

// get is the settings in force, re-read once the ones in hand are old enough, and a config
// that will not load leaves whatever was last read standing.
func (f *frameSettings) get() frames.Settings {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.read.IsZero() && f.now().Sub(f.read) < frameSettingsTTL {
		return f.held
	}

	cfg, err := f.load()
	if err != nil {
		return f.held
	}

	f.read = f.now()
	f.held = frames.Settings{On: cfg.Debug.Frames, Keep: cfg.Debug.FrameHistory}

	return f.held
}
