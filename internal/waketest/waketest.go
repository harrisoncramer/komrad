// Package waketest stands in for the waker of a background loop and the pull request sync.
package waketest

// Counter counts the wakes that callers asked for, and records the hosts they asked it to read often.
type Counter struct {
	Wakes int
	// HostsReadOften is every host asked to be read often, in order.
	HostsReadOften []string
}

// Wake counts one wake.
func (c *Counter) Wake() {
	c.Wakes++
}

// ReadOften records one host asked to be read often.
func (c *Counter) ReadOften(host string) {
	c.HostsReadOften = append(c.HostsReadOften, host)
}
