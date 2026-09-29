// Package waketest stands in for the waker of a background loop.
package waketest

// Counter counts the wakes it was asked for.
type Counter struct {
	Wakes int
}

// Wake counts one wake.
func (c *Counter) Wake() {
	c.Wakes++
}
