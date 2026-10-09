package config

import (
	"fmt"
	"slices"
	"strings"
)

// defaultLogLevel is how much the service writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// service works.
const defaultLogLevel = "info"

// Log is how much the background service writes about itself. Where it writes is not
// configurable.
type Log struct {
	// How much is written, one of debug, info, warn, or error.
	Level string `mapstructure:"level"`
}

// logLevels are the levels the service understands. One it does not know decodes as debug,
// so an unchecked typo turns the log up instead of saying the word was wrong.
var logLevels = []string{"debug", "info", "warn", "error"}

func (l *Log) normalize() {
	if strings.TrimSpace(l.Level) == "" {
		l.Level = defaultLogLevel
	}
}

// validate rejects a level the service could not act on.
func (l Log) validate() error {
	level := strings.TrimSpace(l.Level)
	if level == "" || slices.Contains(logLevels, strings.ToLower(level)) {
		return nil
	}

	return fmt.Errorf("log.level: %q is not one of %s", l.Level, strings.Join(logLevels, ", "))
}

// DefaultFrameHistory is how many frames a recording keeps for one agent when the config names
// no number. Since komrad captures about one frame every two seconds, this equates roughly to 15 minutes
// of frames captured, which should be more than enough to replicate most issues related to state transitions
// or reading data off of an agent panel.
const DefaultFrameHistory = 500

// Debug is what komrad records about its own reading of an agent.
type Debug struct {
	// Whether the pane capture behind each reading of an agent is kept under the cache
	// directory.
	Frames bool `mapstructure:"frames"`
	// How many frames are kept for one agent before the oldest is dropped. A number at or
	// below zero takes the default.
	FrameHistory int `mapstructure:"frame_history"`
}

func (d *Debug) normalize() {
	if d.FrameHistory <= 0 {
		d.FrameHistory = DefaultFrameHistory
	}
}
