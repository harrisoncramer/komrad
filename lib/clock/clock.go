// Package clock holds a sleep that ends with its context.
package clock

import (
	"context"
	"time"
)

// Sleeper pauses its caller.
type Sleeper interface {
	Sleep(ctx context.Context, wait time.Duration)
}

// Wall sleeps on the wall clock.
type Wall struct{}

// Sleep waits for wait to pass or for the context to end, whichever comes first.
func (Wall) Sleep(ctx context.Context, wait time.Duration) {
	Sleep(ctx, wait)
}

// Sleep waits for wait to pass or for the context to end, whichever comes first.
func Sleep(ctx context.Context, wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
