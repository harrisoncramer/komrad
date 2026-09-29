// Package clock holds a sleep that ends with its context.
package clock

import (
	"context"
	"time"
)

// Sleep waits for wait to pass or for the context to end, whichever comes first.
func Sleep(ctx context.Context, wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
