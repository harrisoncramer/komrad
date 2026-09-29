package clock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSleep pins that a sleep ends early once its context is done.
func TestSleep(t *testing.T) {
	tests := []struct {
		name    string
		cancel  bool
		wait    time.Duration
		atLeast time.Duration
		atMost  time.Duration
	}{
		{
			name:   "a context that is already done",
			cancel: true,
			wait:   time.Hour,
			atMost: time.Second,
		},
		{
			name:    "a wait that runs out",
			wait:    10 * time.Millisecond,
			atLeast: 10 * time.Millisecond,
			atMost:  time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			if tt.cancel {
				cancel()
			}
			defer cancel()

			began := time.Now()
			Sleep(ctx, tt.wait)
			slept := time.Since(began)

			assert.GreaterOrEqual(t, slept, tt.atLeast)
			assert.Less(t, slept, tt.atMost)
		})
	}
}
