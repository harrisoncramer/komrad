package sleepskip

import (
	"context"
	"time"
)

func afterReturning(ctx context.Context, wait time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
	}

	return nil
}
