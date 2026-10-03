package sleep

import (
	"context"
	"errors"
	"time"
)

var errStopped = errors.New("stopped")

func afterReturning(ctx context.Context, wait time.Duration) error {
	select { // want `select only sleeps until the context ends: call clock.Sleep and then check ctx.Err`
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
	}

	return nil
}

func reportingOutlived(ctx context.Context, wait time.Duration) bool {
	select { // want `select only sleeps until the context ends`
	case <-time.After(wait):
		return true
	case <-ctx.Done():
		return false
	}
}

func onTimer(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select { // want `select only sleeps until the context ends`
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func inLoop(ctx context.Context, wait time.Duration) {
	for {
		select { // want `select only sleeps until the context ends`
		case <-ctx.Done():
			return
		case <-time.After(wait):
			continue
		}
	}
}

func doingWork(ctx context.Context, wait time.Duration, work func()) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(wait):
		work()
	}
}

func otherChannel(done <-chan struct{}, wait time.Duration) error {
	select {
	case <-done:
		return nil
	case <-time.After(wait):
		return errStopped
	}
}

func threeArms(ctx context.Context, done <-chan struct{}, wait time.Duration) {
	select {
	case <-ctx.Done():
	case <-done:
	case <-time.After(wait):
	}
}

func onTicker(ctx context.Context, ticker *time.Ticker) {
	select {
	case <-ctx.Done():
	case <-ticker.C:
	}
}

func keepingValue(ctx context.Context, wait time.Duration) time.Time {
	select {
	case <-ctx.Done():
		return time.Time{}
	case moment := <-time.After(wait):
		return moment
	}
}
