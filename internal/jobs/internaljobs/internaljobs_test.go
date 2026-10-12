package internaljobs

import (
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
)

// TestStartTimeout covers how long a job that starts an agent may run, which follows the hooks.timeout setting.
func TestStartTimeout(t *testing.T) {
	tests := []struct {
		name        string
		hookTimeout func() (time.Duration, error)
		want        time.Duration
	}{
		{
			name:        "outlasts the provisioner and two moments' hooks",
			hookTimeout: func() (time.Duration, error) { return time.Minute, nil },
			want:        13 * time.Minute,
		},
		{
			name:        "grows with the setting",
			hookTimeout: func() (time.Duration, error) { return 5 * time.Minute, nil },
			want:        21 * time.Minute,
		},
		{
			name:        "leaves River's default where the config cannot be read",
			hookTimeout: func() (time.Duration, error) { return 0, errors.New("no config") },
			want:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wake := NewWakeAgentWorker(nil, tt.hookTimeout)
			launch := NewLaunchAgentWorker(NewLaunchAgentWorkerParams{HookTimeout: tt.hookTimeout})

			assert.Equal(t, tt.want, wake.Timeout(&river.Job[WakeAgentArgs]{}))
			assert.Equal(t, tt.want, launch.Timeout(&river.Job[LaunchAgentArgs]{}))
		})
	}
}
