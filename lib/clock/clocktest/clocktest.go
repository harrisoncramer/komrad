// Package clocktest holds a sleeper that never waits.
package clocktest

import (
	"context"
	"sync/atomic"
	"time"
)

// Sleeper returns from every sleep at once and counts them.
type Sleeper struct {
	slept atomic.Int32
}

// Sleep records the sleep and returns without waiting.
func (s *Sleeper) Sleep(context.Context, time.Duration) {
	s.slept.Add(1)
}

// CountSleeps reports how many sleeps have been asked for.
func (s *Sleeper) CountSleeps() int32 {
	return s.slept.Load()
}
