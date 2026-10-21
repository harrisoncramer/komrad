// Package wake names what asks a background loop to run early or more often.
package wake

// Waker asks a background loop to run before its next tick.
type Waker interface {
	Wake()
}

// HostReader is a background loop that reads a host often for a while when asked.
type HostReader interface {
	ReadOften(host string)
}
