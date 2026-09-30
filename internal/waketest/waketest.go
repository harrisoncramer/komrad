// Package waketest stands in for the waker of a background loop.
package waketest

import "time"

// Counter counts the wakes it was asked for.
type Counter struct {
	Wakes int
	// Delayed is the wait of every wake asked for later, in order.
	Delayed []time.Duration
}

// Wake counts one wake.
func (c *Counter) Wake() {
	c.Wakes++
}

// WakeAfter records a wake asked for once the wait is over.
func (c *Counter) WakeAfter(wait time.Duration) {
	c.Delayed = append(c.Delayed, wait)
}
