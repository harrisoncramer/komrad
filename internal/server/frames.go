package server

import (
	"sync"
	"time"

	"weave/config"
	"weave/internal/frames"
)

// frameSettingsTTL is how long the recording settings are held before the config is read
// again.
const frameSettingsTTL = 5 * time.Second

// frameSettings reads the recording settings from the config on disk rather than at startup, so
// a recording can be turned on under a running service.
type frameSettings struct {
	load func() (*config.Config, error)
	now  func() time.Time

	// mu guards the settings held between reads.
	mu   sync.Mutex
	read time.Time
	held frames.Settings
}

func newFrameSettings(load func() (*config.Config, error)) *frameSettings {
	return &frameSettings{load: load, now: time.Now}
}

// get is the settings in force, re-read once the ones in hand are old enough, a config that will
// not load leaving the last ones standing.
func (f *frameSettings) get() frames.Settings {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.read.IsZero() && f.now().Sub(f.read) < frameSettingsTTL {
		return f.held
	}

	f.read = f.now()

	cfg, err := f.load()
	if err != nil {
		return f.held
	}

	f.held = frames.Settings{On: cfg.Debug.Frames, Keep: cfg.Debug.FrameHistory}

	return f.held
}
