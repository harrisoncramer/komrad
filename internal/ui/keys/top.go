package keys

import (
	"komrad/config"
	"komrad/internal/ui/screen"
)

// Top is whether the last press opened the doubled top key and waits on its pair, shared by
// every screen so the pair holds across a screen change.
type Top struct {
	held bool
}

// DispatchTop settles the doubled top key ahead of the table.
func (s Spec[S]) DispatchTop(top *Top, state S, bindings config.Keybindings, pressed string) screen.Request {
	if !top.take(bindings, pressed) && top.held {
		return nil
	}

	return s.Dispatch(state, bindings, pressed)
}

func (t *Top) take(bindings config.Keybindings, pressed string) bool {
	held := t.held
	t.held = !held && IsBound(pressed, bindings.Nav.Top)

	return held && IsBound(pressed, bindings.Nav.Top)
}

// Drop forgets a first press of the doubled top key.
func (t *Top) Drop() {
	t.held = false
}
