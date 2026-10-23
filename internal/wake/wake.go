package wake

// Waker asks a background loop to run before its next tick.
type Waker interface {
	Wake()
}

// HostReader is a background loop that reads a host at once, and often for a while, when asked.
type HostReader interface {
	ReadOften(host string)
}
