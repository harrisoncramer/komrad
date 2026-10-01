// Package wake names what asks a background loop to run early or more often.
package wake

// Waker asks a background loop to run before its next tick.
type Waker interface {
	Wake()
}

// Heater keeps a background loop reading a host often for a while.
type Heater interface {
	Heat(host string)
}
