// Package wake names what asks a background loop to run early.
package wake

// Waker asks a background loop to run before its next tick.
type Waker interface {
	Wake()
}
