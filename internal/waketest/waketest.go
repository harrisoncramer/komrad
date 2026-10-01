// Package waketest stands in for the waker of a background loop.
package waketest

// Counter counts the wakes it was asked for, and records the hosts it was asked to heat.
type Counter struct {
	Wakes int
	// Heated is every host heated, in order.
	Heated []string
}

// Wake counts one wake.
func (c *Counter) Wake() {
	c.Wakes++
}

// Heat records one host heated.
func (c *Counter) Heat(host string) {
	c.Heated = append(c.Heated, host)
}
