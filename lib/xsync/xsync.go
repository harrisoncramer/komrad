// Package xsync holds small helpers for waiting on goroutines.
package xsync

import "time"

// WithinTimeout runs wait in its own goroutine and reports whether it finished before the
// timeout, leaking that goroutine if it never returns.
func WithinTimeout(within time.Duration, wait func()) bool {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()

	timer := time.NewTimer(within)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
