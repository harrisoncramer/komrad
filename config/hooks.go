package config

import (
	"fmt"
	"strings"
	"time"
)

// defaultHookTimeout is how long a hook may run when the config names no duration.
const defaultHookTimeout = "60s"

// Hooks is where komrad finds the scripts a workflow hangs on a moment in its agents' lives, and
// how long each may run.
type Hooks struct {
	// Directory for hooks: one executable file per hook, named after the hook.
	// A hook is a script or program komrad runs at a moment a workflow names it against, in the
	// agent's own directory, with agent info in KOMRAD_AGENT as JSON and the moment in
	// KOMRAD_TRIGGER. Default is ~/.config/komrad/hooks.
	Dir string `mapstructure:"dir"`
	// How long a hook may run before komrad stops it and counts it as failed, as a Go duration
	// such as "60s" or "5m". A hook's own #KOMRAD timeout can ask for less, never more, and a
	// hook that asks for "none" gets this. A duration komrad cannot read, or one that is not
	// positive, is refused.
	Timeout string `mapstructure:"timeout"`
}

// GetTimeout is how long a hook may run, parsed.
func (h Hooks) GetTimeout() (time.Duration, error) {
	if h.Timeout == "" {
		h.Timeout = defaultHookTimeout
	}

	parsed, err := time.ParseDuration(strings.TrimSpace(h.Timeout))
	if err != nil {
		return 0, fmt.Errorf("unreadable hook timeout %q: %w", h.Timeout, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("the hook timeout must be positive, not %q", h.Timeout)
	}

	return parsed, nil
}

func (h Hooks) validate() error {
	if _, err := h.GetTimeout(); err != nil {
		return fmt.Errorf("hooks.timeout: %w", err)
	}

	return nil
}
