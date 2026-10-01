package boundkey

import "slices"

type NavKeys struct {
	Up   []string
	Down []string
}

type MessagesKeys struct {
	Open    []string
	Dismiss []string
}

type Keybindings struct {
	Nav      NavKeys
	Messages MessagesKeys
}

type FormKeys struct {
	Back   []string
	Submit []string
}

type model struct {
	keys Keybindings
}

type action struct {
	keys func(Keybindings) []string
}

func isBound(pressed string, keys []string) bool {
	return slices.Contains(keys, pressed)
}

func directionOf(pressed string, backward []string) int {
	if isBound(pressed, backward) {
		return -1
	}

	return 1
}

func formKeys() FormKeys {
	return FormKeys{}
}

func (m model) quitKeys() []string {
	return m.keys.Nav.Up
}

func (m model) moves(k string) bool {
	both := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return isBound(k, m.keys.Nav.Down) || isBound(k, both) || isBound(k, formKeys().Back) || isBound(k, m.quitKeys()) || directionOf(k, m.keys.Nav.Up) > 0
}

func (m model) opens(k string) bool {
	return isBound(k, m.keys.Messages.Open)
}

func (m model) dismisses(k string) bool {
	return isBound(k, m.keys.Messages.Dismiss) // want `isBound matches the action key Messages.Dismiss outside a table or a menu entry: put it in the menu, or declare boundkey.model.dismisses an input`
}

func (m model) submits(k string) bool {
	return isBound(k, formKeys().Submit) // want `isBound matches the action key Form.Submit outside a table or a menu entry`
}

func (m model) mixes(k string) bool {
	return directionOf(k, slices.Concat(m.keys.Nav.Up, m.keys.Messages.Dismiss)) > 0 // want `directionOf matches the action key Messages.Dismiss`
}

func (m model) hides(k string, keys []string) bool {
	return isBound(k, keys) // want `isBound matches keys that cannot be traced to the keybindings`
}

func (m model) dispatch(k string, actions []action) bool {
	for _, a := range actions {
		if isBound(k, a.keys(m.keys)) {
			return true
		}
	}

	return false
}

func (m model) save(k string) bool {
	return isBound(k, formKeys().Submit)
}

func (m model) contains(k string, names []string) bool {
	return slices.Contains(names, k) || slices.Contains(m.keys.Nav.Up, k) || slices.Contains(m.keys.Messages.Dismiss, k) // want `Contains matches the action key Messages.Dismiss`
}

func (m model) reassigns(k string) bool {
	keys := m.keys.Nav.Up
	keys = m.keys.Messages.Dismiss

	return isBound(k, keys) // want `isBound matches keys that cannot be traced to the keybindings`
}
