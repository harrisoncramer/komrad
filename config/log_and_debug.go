package config

import (
	"fmt"
	"slices"
	"strings"
)

// defaultLogLevel is how much the server writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// server works.
const defaultLogLevel = "info"

// defaultPprofServerAddress is where the server serves profiles when the config names no address.
const defaultPprofServerAddress = "localhost:6060"

// defaultPprofPanelAddress is where the panel serves profiles when the config names no address.
const defaultPprofPanelAddress = "localhost:6061"

// Log is how much the server writes about itself. Where it writes is not
// configurable.
type Log struct {
	// The amount komrad writes, one of debug, info, warn, or error.
	Level string `mapstructure:"level"`
}

// logLevels are the levels the server understands. One it does not know decodes as debug,
// so an unchecked typo turns the log up instead of saying the word was wrong.
var logLevels = []string{"debug", "info", "warn", "error"}

func (l *Log) normalize() {
	if strings.TrimSpace(l.Level) == "" {
		l.Level = defaultLogLevel
	}
}

// validate rejects a level the server could not act on.
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
	// Keeps the pane capture behind each reading of an agent under the cache directory when on.
	Frames bool `mapstructure:"frames"`
	// The number of frames komrad keeps for one agent before it drops the oldest. A number at or
	// below zero takes the default.
	FrameHistory int `mapstructure:"frame_history"`
	// Go's pprof profiler, served by the server and the panel.
	Pprof Pprof `mapstructure:"pprof"`
}

// Pprof is where each komrad process serves its runtime profiles.
type Pprof struct {
	// The server, which runs the agents and answers the API.
	Server Profiler `mapstructure:"server"`
	// The panel, which draws the screen and talks to the server.
	Panel Profiler `mapstructure:"panel"`
}

// Profiler is whether and where one process serves its runtime profiles.
type Profiler struct {
	// Serves profiles from the process when on. Komrad reads it when the process starts.
	Enabled bool `mapstructure:"enabled"`
	// The host and port the profiles are served on.
	Address string `mapstructure:"address"`
}

func (d *Debug) normalize() {
	if d.FrameHistory <= 0 {
		d.FrameHistory = DefaultFrameHistory
	}

	if d.Pprof.Server.Address == "" {
		d.Pprof.Server.Address = defaultPprofServerAddress
	}

	if d.Pprof.Panel.Address == "" {
		d.Pprof.Panel.Address = defaultPprofPanelAddress
	}
}
