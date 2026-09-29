// Package clock is where domain code reads the wall clock and waits on it.
package clock

import (
	"context"
	"time"
)

// Now returns the current wall-clock time.
func Now() time.Time {
	return time.Now()
}

// Since returns how long has passed since began.
func Since(began time.Time) time.Duration {
	return time.Since(began)
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
